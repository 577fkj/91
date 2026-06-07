// Package driveplugin defines the public HashiCorp go-plugin protocol for
// user supplied drive backends.
package driveplugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/rpc"
	"os"
	"time"

	hplugin "github.com/hashicorp/go-plugin"
)

const (
	PluginName      = "drive"
	ProtocolVersion = 1
)

var (
	HandshakeConfig = hplugin.HandshakeConfig{
		ProtocolVersion:  ProtocolVersion,
		MagicCookieKey:   "VIDEO_SITE_DRIVE_PLUGIN",
		MagicCookieValue: "video-site-drive-plugin-v1",
	}

	ErrNotSupported = errors.New("operation not supported by this drive")
)

type Config struct {
	ID     string
	Kind   string
	RootID string
	Params map[string]string
}

type Info struct {
	ID          string
	Kind        string
	Name        string
	Description string
	Version     string
}

type InfoProvider interface {
	Info(ctx context.Context) (Info, error)
}

type Driver interface {
	Configure(ctx context.Context, cfg Config) error
	Kind() string
	ID() string
	Init(ctx context.Context) error
	List(ctx context.Context, dirID string) ([]Entry, error)
	Stat(ctx context.Context, fileID string) (*Entry, error)
	StreamURL(ctx context.Context, fileID string) (*StreamLink, error)
	Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (string, error)
	EnsureDir(ctx context.Context, pathFromRoot string) (string, error)
	RootID() string
}

type EntryTagProvider interface {
	EntryTags(ctx context.Context, entry Entry) ([]string, error)
}

type EntryTitleProvider interface {
	EntryTitle(ctx context.Context, entry Entry) (string, error)
}

type StreamURLWithHeaderProvider interface {
	StreamURLWithHeader(ctx context.Context, fileID string, header http.Header) (*StreamLink, error)
}

type Entry struct {
	ID           string
	Name         string
	Title        string
	Size         int64
	Hash         string
	IsDir        bool
	ParentID     string
	MimeType     string
	ModTime      time.Time
	Category     int
	ThumbnailURL string
}

type StreamLink struct {
	URL     string
	Headers http.Header
	Expires time.Time
}

type Capabilities struct {
	EntryTags           bool
	EntryTitle          bool
	StreamURLWithHeader bool
}

type Plugin struct {
	Impl Driver
}

func (p *Plugin) Server(*hplugin.MuxBroker) (interface{}, error) {
	if p.Impl == nil {
		return nil, errors.New("drive plugin implementation is nil")
	}
	return &RPCServer{Impl: p.Impl}, nil
}

func (p *Plugin) Client(_ *hplugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &RPCClient{client: c}, nil
}

func PluginMap(impl Driver) map[string]hplugin.Plugin {
	return map[string]hplugin.Plugin{
		PluginName: &Plugin{Impl: impl},
	}
}

func Serve(impl Driver) {
	hplugin.Serve(&hplugin.ServeConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins:         PluginMap(impl),
	})
}

type Empty struct{}

type StringReply struct {
	Value string
}

type ListArgs struct {
	DirID string
}

type ListReply struct {
	Entries []Entry
}

type StatArgs struct {
	FileID string
}

type EntryReply struct {
	Entry *Entry
}

type StreamURLArgs struct {
	FileID string
}

type StreamURLWithHeaderArgs struct {
	FileID string
	Header http.Header
}

type StreamLinkReply struct {
	Link *StreamLink
}

type UploadPathArgs struct {
	ParentID string
	Name     string
	Path     string
	Size     int64
}

type UploadReply struct {
	FileID string
}

type EnsureDirArgs struct {
	PathFromRoot string
}

type CapabilitiesReply struct {
	Capabilities Capabilities
}

type InfoReply struct {
	Info Info
}

type EntryTagsArgs struct {
	Entry Entry
}

type EntryTagsReply struct {
	Tags []string
}

type EntryTitleArgs struct {
	Entry Entry
}

type EntryTitleReply struct {
	Title string
}

type RPCServer struct {
	Impl Driver
}

func (s *RPCServer) Configure(args Config, _ *Empty) error {
	return s.Impl.Configure(context.Background(), args)
}

func (s *RPCServer) Info(_ Empty, reply *InfoReply) error {
	if provider, ok := s.Impl.(InfoProvider); ok {
		info, err := provider.Info(context.Background())
		if err != nil {
			return err
		}
		reply.Info = info
		return nil
	}
	info := Info{
		Kind: s.Impl.Kind(),
		Name: s.Impl.Kind(),
	}
	if info.Kind == "" {
		return errors.New("drive plugin info is not available before configure")
	}
	reply.Info = info
	return nil
}

func (s *RPCServer) Kind(_ Empty, reply *StringReply) error {
	reply.Value = s.Impl.Kind()
	return nil
}

func (s *RPCServer) ID(_ Empty, reply *StringReply) error {
	reply.Value = s.Impl.ID()
	return nil
}

func (s *RPCServer) Init(_ Empty, _ *Empty) error {
	return s.Impl.Init(context.Background())
}

func (s *RPCServer) List(args ListArgs, reply *ListReply) error {
	entries, err := s.Impl.List(context.Background(), args.DirID)
	if err != nil {
		return err
	}
	reply.Entries = entries
	return nil
}

func (s *RPCServer) Stat(args StatArgs, reply *EntryReply) error {
	entry, err := s.Impl.Stat(context.Background(), args.FileID)
	if err != nil {
		return err
	}
	reply.Entry = entry
	return nil
}

func (s *RPCServer) StreamURL(args StreamURLArgs, reply *StreamLinkReply) error {
	link, err := s.Impl.StreamURL(context.Background(), args.FileID)
	if err != nil {
		return err
	}
	reply.Link = link
	return nil
}

func (s *RPCServer) UploadPath(args UploadPathArgs, reply *UploadReply) error {
	f, err := os.Open(args.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	fileID, err := s.Impl.Upload(context.Background(), args.ParentID, args.Name, f, args.Size)
	if err != nil {
		return err
	}
	reply.FileID = fileID
	return nil
}

func (s *RPCServer) EnsureDir(args EnsureDirArgs, reply *StringReply) error {
	id, err := s.Impl.EnsureDir(context.Background(), args.PathFromRoot)
	if err != nil {
		return err
	}
	reply.Value = id
	return nil
}

func (s *RPCServer) RootID(_ Empty, reply *StringReply) error {
	reply.Value = s.Impl.RootID()
	return nil
}

func (s *RPCServer) Capabilities(_ Empty, reply *CapabilitiesReply) error {
	_, entryTags := s.Impl.(EntryTagProvider)
	_, entryTitle := s.Impl.(EntryTitleProvider)
	_, streamURLWithHeader := s.Impl.(StreamURLWithHeaderProvider)
	reply.Capabilities = Capabilities{
		EntryTags:           entryTags,
		EntryTitle:          entryTitle,
		StreamURLWithHeader: streamURLWithHeader,
	}
	return nil
}

func (s *RPCServer) EntryTags(args EntryTagsArgs, reply *EntryTagsReply) error {
	provider, ok := s.Impl.(EntryTagProvider)
	if !ok {
		return ErrNotSupported
	}
	tags, err := provider.EntryTags(context.Background(), args.Entry)
	if err != nil {
		return err
	}
	reply.Tags = tags
	return nil
}

func (s *RPCServer) EntryTitle(args EntryTitleArgs, reply *EntryTitleReply) error {
	provider, ok := s.Impl.(EntryTitleProvider)
	if !ok {
		return ErrNotSupported
	}
	title, err := provider.EntryTitle(context.Background(), args.Entry)
	if err != nil {
		return err
	}
	reply.Title = title
	return nil
}

func (s *RPCServer) StreamURLWithHeader(args StreamURLWithHeaderArgs, reply *StreamLinkReply) error {
	provider, ok := s.Impl.(StreamURLWithHeaderProvider)
	if !ok {
		return ErrNotSupported
	}
	link, err := provider.StreamURLWithHeader(context.Background(), args.FileID, args.Header)
	if err != nil {
		return err
	}
	reply.Link = link
	return nil
}

type RPCClient struct {
	client *rpc.Client
}

func (c *RPCClient) Configure(ctx context.Context, cfg Config) error {
	return c.call(ctx, "Configure", cfg, &Empty{})
}

func (c *RPCClient) Info(ctx context.Context) (Info, error) {
	var reply InfoReply
	if err := c.call(ctx, "Info", Empty{}, &reply); err != nil {
		return Info{}, err
	}
	return reply.Info, nil
}

func (c *RPCClient) Kind() string {
	var reply StringReply
	if err := c.call(context.Background(), "Kind", Empty{}, &reply); err != nil {
		return ""
	}
	return reply.Value
}

func (c *RPCClient) ID() string {
	var reply StringReply
	if err := c.call(context.Background(), "ID", Empty{}, &reply); err != nil {
		return ""
	}
	return reply.Value
}

func (c *RPCClient) Init(ctx context.Context) error {
	return c.call(ctx, "Init", Empty{}, &Empty{})
}

func (c *RPCClient) List(ctx context.Context, dirID string) ([]Entry, error) {
	var reply ListReply
	if err := c.call(ctx, "List", ListArgs{DirID: dirID}, &reply); err != nil {
		return nil, err
	}
	return reply.Entries, nil
}

func (c *RPCClient) Stat(ctx context.Context, fileID string) (*Entry, error) {
	var reply EntryReply
	if err := c.call(ctx, "Stat", StatArgs{FileID: fileID}, &reply); err != nil {
		return nil, err
	}
	return reply.Entry, nil
}

func (c *RPCClient) StreamURL(ctx context.Context, fileID string) (*StreamLink, error) {
	var reply StreamLinkReply
	if err := c.call(ctx, "StreamURL", StreamURLArgs{FileID: fileID}, &reply); err != nil {
		return nil, err
	}
	return reply.Link, nil
}

func (c *RPCClient) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (string, error) {
	if r == nil {
		return "", errors.New("upload reader is nil")
	}
	tmp, err := os.CreateTemp("", "video-site-drive-plugin-upload-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	written, copyErr := io.Copy(tmp, r)
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if size >= 0 && written != size {
		return "", fmt.Errorf("upload size mismatch: wrote %d bytes, expected %d", written, size)
	}

	var reply UploadReply
	err = c.call(ctx, "UploadPath", UploadPathArgs{
		ParentID: parentID,
		Name:     name,
		Path:     tmpPath,
		Size:     written,
	}, &reply)
	if err != nil {
		return "", err
	}
	return reply.FileID, nil
}

func (c *RPCClient) EnsureDir(ctx context.Context, pathFromRoot string) (string, error) {
	var reply StringReply
	if err := c.call(ctx, "EnsureDir", EnsureDirArgs{PathFromRoot: pathFromRoot}, &reply); err != nil {
		return "", err
	}
	return reply.Value, nil
}

func (c *RPCClient) RootID() string {
	var reply StringReply
	if err := c.call(context.Background(), "RootID", Empty{}, &reply); err != nil {
		return ""
	}
	return reply.Value
}

func (c *RPCClient) Capabilities(ctx context.Context) (Capabilities, error) {
	var reply CapabilitiesReply
	if err := c.call(ctx, "Capabilities", Empty{}, &reply); err != nil {
		return Capabilities{}, err
	}
	return reply.Capabilities, nil
}

func (c *RPCClient) EntryTags(ctx context.Context, entry Entry) ([]string, error) {
	var reply EntryTagsReply
	if err := c.call(ctx, "EntryTags", EntryTagsArgs{Entry: entry}, &reply); err != nil {
		return nil, err
	}
	return reply.Tags, nil
}

func (c *RPCClient) EntryTitle(ctx context.Context, entry Entry) (string, error) {
	var reply EntryTitleReply
	if err := c.call(ctx, "EntryTitle", EntryTitleArgs{Entry: entry}, &reply); err != nil {
		return "", err
	}
	return reply.Title, nil
}

func (c *RPCClient) StreamURLWithHeader(ctx context.Context, fileID string, header http.Header) (*StreamLink, error) {
	var reply StreamLinkReply
	if err := c.call(ctx, "StreamURLWithHeader", StreamURLWithHeaderArgs{FileID: fileID, Header: header}, &reply); err != nil {
		return nil, err
	}
	return reply.Link, nil
}

func (c *RPCClient) call(ctx context.Context, method string, args any, reply any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan *rpc.Call, 1)
	call := c.client.Go("Plugin."+method, args, reply, done)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-call.Done:
		return normalizeRPCError(result.Error)
	}
}

func normalizeRPCError(err error) error {
	if err == nil {
		return nil
	}
	if err.Error() == ErrNotSupported.Error() {
		return ErrNotSupported
	}
	return err
}

var _ hplugin.Plugin = (*Plugin)(nil)
var _ Driver = (*RPCClient)(nil)
