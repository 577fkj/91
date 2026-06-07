package pluginhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	hplugin "github.com/hashicorp/go-plugin"

	"github.com/video-site/backend/internal/drives"
	"github.com/video-site/backend/pkg/driveplugin"
)

const Kind = "plugin"

const defaultPluginStartTimeout = 10 * time.Second

type Config struct {
	ID      string
	Plugin  string
	Kind    string
	RootID  string
	Command string
	Args    []string
	Params  map[string]string
}

type Definition struct {
	ID          string
	Kind        string
	Name        string
	Description string
	Version     string
	Command     string
	Args        []string
	Source      string
}

type Registry struct {
	byID   map[string]Definition
	byKind map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{
		byID:   make(map[string]Definition),
		byKind: make(map[string]Definition),
	}
}

func (r *Registry) Register(def Definition) error {
	if r == nil {
		return errors.New("plugin registry is nil")
	}
	def.ID = strings.TrimSpace(def.ID)
	def.Kind = strings.TrimSpace(def.Kind)
	def.Command = strings.TrimSpace(def.Command)
	if def.ID == "" {
		def.ID = def.Kind
	}
	if def.Kind == "" {
		return errors.New("plugin definition kind is required")
	}
	if def.Command == "" {
		return fmt.Errorf("plugin %s command is required", def.Kind)
	}
	if existing, ok := r.byKind[def.Kind]; ok && existing.Command != def.Command {
		return fmt.Errorf("plugin kind %q already registered from %s", def.Kind, existing.Command)
	}
	if existing, ok := r.byID[def.ID]; ok && existing.Command != def.Command {
		return fmt.Errorf("plugin id %q already registered from %s", def.ID, existing.Command)
	}
	r.byID[def.ID] = def
	r.byKind[def.Kind] = def
	return nil
}

func (r *Registry) Get(ref string) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Definition{}, false
	}
	if def, ok := r.byID[ref]; ok {
		return def, true
	}
	def, ok := r.byKind[ref]
	return def, ok
}

func (r *Registry) List() []Definition {
	if r == nil {
		return nil
	}
	out := make([]Definition, 0, len(r.byKind))
	for _, def := range r.byKind {
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Kind < out[j].Kind
	})
	return out
}

func (r *Registry) Discover(ctx context.Context, dirs []string) error {
	var errs []error
	for _, dir := range dirs {
		defs, err := DiscoverDir(ctx, dir)
		if err != nil {
			errs = append(errs, err)
		}
		for _, def := range defs {
			if err := r.Register(def); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func DiscoverDir(ctx context.Context, dir string) ([]Definition, error) {
	dir = strings.TrimSpace(os.ExpandEnv(dir))
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan drive plugin dir %s: %w", dir, err)
	}
	var (
		defs []Definition
		errs []error
	)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			errs = append(errs, fmt.Errorf("stat plugin candidate %s: %w", entry.Name(), err))
			continue
		}
		if !looksExecutable(entry.Name(), info.Mode()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		def, err := Probe(ctx, path, nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("probe drive plugin %s: %w", path, err))
			continue
		}
		def.Source = dir
		defs = append(defs, def)
	}
	return defs, errors.Join(errs...)
}

func Probe(ctx context.Context, command string, args []string) (Definition, error) {
	command = strings.TrimSpace(os.ExpandEnv(command))
	if command == "" {
		return Definition{}, errors.New("plugin command is required")
	}
	client := hplugin.NewClient(&hplugin.ClientConfig{
		HandshakeConfig:  driveplugin.HandshakeConfig,
		Plugins:          driveplugin.PluginMap(nil),
		Cmd:              exec.Command(command, args...),
		AllowedProtocols: []hplugin.Protocol{hplugin.ProtocolNetRPC},
		StartTimeout:     defaultPluginStartTimeout,
	})
	defer client.Kill()

	rpcClient, err := client.Client()
	if err != nil {
		return Definition{}, err
	}
	raw, err := rpcClient.Dispense(driveplugin.PluginName)
	if err != nil {
		return Definition{}, err
	}
	remote, ok := raw.(interface {
		Info(ctx context.Context) (driveplugin.Info, error)
	})
	if !ok {
		return Definition{}, fmt.Errorf("drive plugin client missing Info")
	}
	probeCtx, cancel := context.WithTimeout(ctx, defaultPluginStartTimeout)
	defer cancel()
	info, err := remote.Info(probeCtx)
	if err != nil {
		return Definition{}, err
	}
	def := Definition{
		ID:          strings.TrimSpace(info.ID),
		Kind:        strings.TrimSpace(info.Kind),
		Name:        strings.TrimSpace(info.Name),
		Description: strings.TrimSpace(info.Description),
		Version:     strings.TrimSpace(info.Version),
		Command:     command,
		Args:        append([]string{}, args...),
	}
	if def.Kind == "" {
		return Definition{}, errors.New("plugin info kind is required")
	}
	if def.ID == "" {
		def.ID = def.Kind
	}
	if def.Name == "" {
		def.Name = def.Kind
	}
	return def, nil
}

type capabilitiesProvider interface {
	Capabilities(ctx context.Context) (driveplugin.Capabilities, error)
}

type entryTagsProvider interface {
	EntryTags(ctx context.Context, entry driveplugin.Entry) ([]string, error)
}

type streamURLWithHeaderProvider interface {
	StreamURLWithHeader(ctx context.Context, fileID string, header http.Header) (*driveplugin.StreamLink, error)
}

type baseDriver struct {
	id     string
	kind   string
	rootID string
	remote driveplugin.Driver
	client *hplugin.Client
}

func New(ctx context.Context, cfg Config) (drives.Drive, error) {
	return newWithCommand(ctx, cfg, cfg.Command, cfg.Args)
}

func NewFromDefinition(ctx context.Context, def Definition, cfg Config) (drives.Drive, error) {
	if cfg.Kind == "" {
		cfg.Kind = def.Kind
	}
	return newWithCommand(ctx, cfg, def.Command, append([]string{}, def.Args...))
}

func newWithCommand(ctx context.Context, cfg Config, command string, args []string) (drives.Drive, error) {
	if strings.TrimSpace(cfg.ID) == "" {
		return nil, errors.New("plugin drive id is required")
	}
	command = strings.TrimSpace(os.ExpandEnv(command))
	if command == "" {
		return nil, errors.New("plugin drive command is required")
	}
	pluginKind := strings.TrimSpace(cfg.Kind)
	if pluginKind == "" {
		pluginKind = Kind
	}
	params := cfg.Params
	if params == nil {
		params = map[string]string{}
	}

	client := hplugin.NewClient(&hplugin.ClientConfig{
		HandshakeConfig:  driveplugin.HandshakeConfig,
		Plugins:          driveplugin.PluginMap(nil),
		Cmd:              exec.Command(command, args...),
		AllowedProtocols: []hplugin.Protocol{hplugin.ProtocolNetRPC},
		StartTimeout:     defaultPluginStartTimeout,
	})
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("start drive plugin %q: %w", command, err)
	}
	raw, err := rpcClient.Dispense(driveplugin.PluginName)
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("dispense drive plugin %q: %w", command, err)
	}
	remote, ok := raw.(driveplugin.Driver)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("drive plugin %q returned %T", command, raw)
	}
	if err := remote.Configure(ctx, driveplugin.Config{
		ID:     cfg.ID,
		Kind:   pluginKind,
		RootID: cfg.RootID,
		Params: params,
	}); err != nil {
		client.Kill()
		return nil, fmt.Errorf("configure drive plugin %q: %w", command, err)
	}

	kind := strings.TrimSpace(remote.Kind())
	if kind == "" {
		kind = pluginKind
	}
	rootID := strings.TrimSpace(cfg.RootID)
	if rootID == "" {
		rootID = strings.TrimSpace(remote.RootID())
	}
	base := &baseDriver{
		id:     cfg.ID,
		kind:   kind,
		rootID: rootID,
		remote: remote,
		client: client,
	}

	caps := driveplugin.Capabilities{}
	if provider, ok := remote.(capabilitiesProvider); ok {
		if got, err := provider.Capabilities(ctx); err == nil {
			caps = got
		}
	}
	switch {
	case caps.EntryTags && caps.StreamURLWithHeader:
		return &taggedStreamDriver{baseDriver: base}, nil
	case caps.EntryTags:
		return &taggedDriver{baseDriver: base}, nil
	case caps.StreamURLWithHeader:
		return &streamDriver{baseDriver: base}, nil
	default:
		return base, nil
	}
}

func ConfigFromCredentials(id, rootID string, credentials map[string]string) (Config, error) {
	command := strings.TrimSpace(credentials["command"])
	args, err := parseArgs(credentials["args"])
	if err != nil {
		return Config{}, err
	}
	params := map[string]string{}
	for k, v := range credentials {
		switch k {
		case "command", "args", "plugin_kind", "params_json":
			continue
		default:
			params[k] = v
		}
	}
	if err := mergeParamsJSON(params, credentials["params_json"]); err != nil {
		return Config{}, err
	}
	return Config{
		ID:      id,
		Plugin:  pluginRef(credentials, command),
		Kind:    strings.TrimSpace(credentials["plugin_kind"]),
		RootID:  rootID,
		Command: command,
		Args:    args,
		Params:  params,
	}, nil
}

func pluginRef(credentials map[string]string, command string) string {
	for _, key := range []string{"plugin", "plugin_id"} {
		if ref := strings.TrimSpace(credentials[key]); ref != "" {
			return ref
		}
	}
	if strings.TrimSpace(command) == "" {
		return strings.TrimSpace(credentials["plugin_kind"])
	}
	return ""
}

func (d *baseDriver) Kind() string { return d.kind }

func (d *baseDriver) ID() string { return d.id }

func (d *baseDriver) RootID() string {
	if d.rootID != "" {
		return d.rootID
	}
	return d.remote.RootID()
}

func (d *baseDriver) Init(ctx context.Context) error {
	if err := d.remote.Init(ctx); err != nil {
		return mapPluginError(err)
	}
	if d.rootID == "" {
		d.rootID = strings.TrimSpace(d.remote.RootID())
	}
	return nil
}

func (d *baseDriver) List(ctx context.Context, dirID string) ([]drives.Entry, error) {
	entries, err := d.remote.List(ctx, dirID)
	if err != nil {
		return nil, mapPluginError(err)
	}
	out := make([]drives.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, fromPluginEntry(e))
	}
	return out, nil
}

func (d *baseDriver) Stat(ctx context.Context, fileID string) (*drives.Entry, error) {
	entry, err := d.remote.Stat(ctx, fileID)
	if err != nil {
		return nil, mapPluginError(err)
	}
	if entry == nil {
		return nil, nil
	}
	got := fromPluginEntry(*entry)
	return &got, nil
}

func (d *baseDriver) StreamURL(ctx context.Context, fileID string) (*drives.StreamLink, error) {
	link, err := d.remote.StreamURL(ctx, fileID)
	if err != nil {
		return nil, mapPluginError(err)
	}
	return fromPluginStreamLink(link), nil
}

func (d *baseDriver) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (string, error) {
	id, err := d.remote.Upload(ctx, parentID, name, r, size)
	if err != nil {
		return "", mapPluginError(err)
	}
	return id, nil
}

func (d *baseDriver) EnsureDir(ctx context.Context, pathFromRoot string) (string, error) {
	id, err := d.remote.EnsureDir(ctx, pathFromRoot)
	if err != nil {
		return "", mapPluginError(err)
	}
	return id, nil
}

func (d *baseDriver) Close() error {
	if d.client != nil {
		d.client.Kill()
	}
	return nil
}

type taggedDriver struct {
	*baseDriver
}

func (d *taggedDriver) EntryTags(ctx context.Context, entry drives.Entry) ([]string, error) {
	provider, ok := d.remote.(entryTagsProvider)
	if !ok {
		return nil, nil
	}
	tags, err := provider.EntryTags(ctx, toPluginEntry(entry))
	if err != nil {
		return nil, mapPluginError(err)
	}
	return tags, nil
}

type streamDriver struct {
	*baseDriver
}

func (d *streamDriver) StreamURLWithHeader(ctx context.Context, fileID string, header http.Header) (*drives.StreamLink, error) {
	provider, ok := d.remote.(streamURLWithHeaderProvider)
	if !ok {
		return d.StreamURL(ctx, fileID)
	}
	link, err := provider.StreamURLWithHeader(ctx, fileID, header)
	if err != nil {
		return nil, mapPluginError(err)
	}
	return fromPluginStreamLink(link), nil
}

type taggedStreamDriver struct {
	*baseDriver
}

func (d *taggedStreamDriver) EntryTags(ctx context.Context, entry drives.Entry) ([]string, error) {
	provider, ok := d.remote.(entryTagsProvider)
	if !ok {
		return nil, nil
	}
	tags, err := provider.EntryTags(ctx, toPluginEntry(entry))
	if err != nil {
		return nil, mapPluginError(err)
	}
	return tags, nil
}

func (d *taggedStreamDriver) StreamURLWithHeader(ctx context.Context, fileID string, header http.Header) (*drives.StreamLink, error) {
	provider, ok := d.remote.(streamURLWithHeaderProvider)
	if !ok {
		return d.StreamURL(ctx, fileID)
	}
	link, err := provider.StreamURLWithHeader(ctx, fileID, header)
	if err != nil {
		return nil, mapPluginError(err)
	}
	return fromPluginStreamLink(link), nil
}

func fromPluginEntry(e driveplugin.Entry) drives.Entry {
	return drives.Entry{
		ID:           e.ID,
		Name:         e.Name,
		Size:         e.Size,
		Hash:         e.Hash,
		IsDir:        e.IsDir,
		ParentID:     e.ParentID,
		MimeType:     e.MimeType,
		ModTime:      e.ModTime,
		Category:     e.Category,
		ThumbnailURL: e.ThumbnailURL,
	}
}

func toPluginEntry(e drives.Entry) driveplugin.Entry {
	return driveplugin.Entry{
		ID:           e.ID,
		Name:         e.Name,
		Size:         e.Size,
		Hash:         e.Hash,
		IsDir:        e.IsDir,
		ParentID:     e.ParentID,
		MimeType:     e.MimeType,
		ModTime:      e.ModTime,
		Category:     e.Category,
		ThumbnailURL: e.ThumbnailURL,
	}
}

func fromPluginStreamLink(link *driveplugin.StreamLink) *drives.StreamLink {
	if link == nil {
		return nil
	}
	return &drives.StreamLink{
		URL:     link.URL,
		Headers: link.Headers,
		Expires: link.Expires,
	}
}

func mapPluginError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, driveplugin.ErrNotSupported) || err.Error() == driveplugin.ErrNotSupported.Error() {
		return drives.ErrNotSupported
	}
	return err
}

func mergeParamsJSON(params map[string]string, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return fmt.Errorf("parse plugin params_json: %w", err)
	}
	for k, v := range decoded {
		switch typed := v.(type) {
		case nil:
			params[k] = ""
		case string:
			params[k] = typed
		default:
			b, err := json.Marshal(typed)
			if err != nil {
				params[k] = fmt.Sprint(typed)
			} else {
				params[k] = string(b)
			}
		}
	}
	return nil
}

func parseArgs(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var args []string
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return nil, fmt.Errorf("parse plugin args JSON: %w", err)
		}
		return args, nil
	}
	return splitArgs(raw)
}

func looksExecutable(name string, mode os.FileMode) bool {
	if !mode.IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(name))
		return ext == ".exe"
	}
	return mode&0o111 != 0
}

func splitArgs(raw string) ([]string, error) {
	var (
		args  []string
		part  strings.Builder
		quote rune
	)
	flush := func() {
		if part.Len() > 0 {
			args = append(args, part.String())
			part.Reset()
		}
	}
	for _, r := range raw {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				part.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
		case unicode.IsSpace(r):
			flush()
		default:
			part.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, errors.New("parse plugin args: unclosed quote")
	}
	flush()
	return args, nil
}

var _ drives.Drive = (*baseDriver)(nil)
var _ drives.EntryTagProvider = (*taggedDriver)(nil)
