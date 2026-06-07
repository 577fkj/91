package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/video-site/backend/pkg/driveplugin"
)

type Driver struct {
	cfg    driveplugin.Config
	rootID string
	entry  driveplugin.Entry
	url    string
	tags   []string
}

func main() {
	driveplugin.Serve(&Driver{})
}

func (d *Driver) Info(context.Context) (driveplugin.Info, error) {
	return driveplugin.Info{
		ID:          "staticdrive",
		Kind:        "staticdrive",
		Name:        "Static Drive",
		Description: "Serves one static video URL as a drive.",
		Version:     "0.1.0",
	}, nil
}

func (d *Driver) Configure(_ context.Context, cfg driveplugin.Config) error {
	d.cfg = cfg
	d.rootID = strings.TrimSpace(cfg.RootID)
	if d.rootID == "" {
		d.rootID = "root"
	}

	name := strings.TrimSpace(cfg.Params["name"])
	if name == "" {
		name = "sample.mp4"
	}
	fileID := strings.TrimSpace(cfg.Params["file_id"])
	if fileID == "" {
		fileID = name
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(cfg.Params["size"]), 10, 64)
	d.entry = driveplugin.Entry{
		ID:       fileID,
		Name:     name,
		Title:    strings.TrimSpace(cfg.Params["title"]),
		Size:     size,
		Hash:     strings.TrimSpace(cfg.Params["hash"]),
		ParentID: d.rootID,
		MimeType: "video/mp4",
		ModTime:  time.Now(),
	}
	d.url = strings.TrimSpace(cfg.Params["url"])
	if rawTags := strings.TrimSpace(cfg.Params["tags"]); rawTags != "" {
		for _, tag := range strings.Split(rawTags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				d.tags = append(d.tags, tag)
			}
		}
	}
	return nil
}

func (d *Driver) Kind() string {
	if d.cfg.Kind != "" {
		return d.cfg.Kind
	}
	return "staticdrive"
}

func (d *Driver) ID() string { return d.cfg.ID }

func (d *Driver) RootID() string { return d.rootID }

func (d *Driver) Init(context.Context) error {
	if d.url == "" {
		return errors.New("static drive: url param is required")
	}
	if d.entry.Size <= 0 {
		return errors.New("static drive: positive size param is required")
	}
	return nil
}

func (d *Driver) List(_ context.Context, dirID string) ([]driveplugin.Entry, error) {
	if dirID == "" {
		dirID = d.rootID
	}
	if dirID != d.rootID {
		return []driveplugin.Entry{}, nil
	}
	return []driveplugin.Entry{d.entry}, nil
}

func (d *Driver) Stat(_ context.Context, fileID string) (*driveplugin.Entry, error) {
	if fileID != d.entry.ID {
		return nil, os.ErrNotExist
	}
	entry := d.entry
	return &entry, nil
}

func (d *Driver) StreamURL(_ context.Context, fileID string) (*driveplugin.StreamLink, error) {
	if fileID != d.entry.ID {
		return nil, os.ErrNotExist
	}
	return &driveplugin.StreamLink{
		URL:     d.url,
		Expires: time.Now().Add(time.Hour),
	}, nil
}

func (d *Driver) Upload(context.Context, string, string, io.Reader, int64) (string, error) {
	return "", driveplugin.ErrNotSupported
}

func (d *Driver) EnsureDir(context.Context, string) (string, error) {
	return "", driveplugin.ErrNotSupported
}

func (d *Driver) EntryTags(context.Context, driveplugin.Entry) ([]string, error) {
	return append([]string{}, d.tags...), nil
}

func (d *Driver) EntryTitle(context.Context, driveplugin.Entry) (string, error) {
	return d.entry.Title, nil
}
