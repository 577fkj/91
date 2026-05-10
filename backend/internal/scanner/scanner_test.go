package scanner

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/drives"
)

func TestRunPersistsRemoteThumbnailFromDriveEntry(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	drv := &scannerFakeDrive{
		entries: []drives.Entry{{
			ID:           "file-1",
			Name:         "clip.mp4",
			Size:         123,
			MimeType:     "video/mp4",
			ModTime:      time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
			ThumbnailURL: "https://thumbnail.example/clip.jpg",
		}},
	}
	sc := New(cat, drv, []string{".mp4"}, 5, nil)

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Added != 1 {
		t.Fatalf("added = %d, want 1", stats.Added)
	}

	got, err := cat.GetVideo(ctx, "fake-drive-file-1")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if got.ThumbnailURL != "https://thumbnail.example/clip.jpg" {
		t.Fatalf("thumbnail = %q, want remote thumbnail", got.ThumbnailURL)
	}
}

func TestRunBackfillsRemoteThumbnailForExistingVideo(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID:            "fake-drive-file-1",
		DriveID:       "drive",
		FileID:        "file-1",
		Title:         "Clip",
		PreviewStatus: "pending",
		PublishedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}

	drv := &scannerFakeDrive{
		entries: []drives.Entry{{
			ID:           "file-1",
			Name:         "clip.mp4",
			Size:         123,
			MimeType:     "video/mp4",
			ModTime:      now,
			ThumbnailURL: "https://thumbnail.example/backfilled.jpg",
		}},
	}
	sc := New(cat, drv, []string{".mp4"}, 5, nil)

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Added != 0 {
		t.Fatalf("added = %d, want 0", stats.Added)
	}

	got, err := cat.GetVideo(ctx, "fake-drive-file-1")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if got.ThumbnailURL != "https://thumbnail.example/backfilled.jpg" {
		t.Fatalf("thumbnail = %q, want backfilled remote thumbnail", got.ThumbnailURL)
	}
}

func TestRunReplacesExistingVideoTagsWithFixedFilenameTags(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID:            "fake-drive-file-1",
		DriveID:       "drive",
		FileID:        "file-1",
		Title:         "Old",
		Tags:          []string{"sunny", "kenny"},
		PreviewStatus: "pending",
		PublishedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}

	drv := &scannerFakeDrive{
		entries: []drives.Entry{{
			ID:      "file-1",
			Name:    "女大后入.mp4",
			Size:    123,
			ModTime: now,
		}},
	}
	sc := New(cat, drv, []string{".mp4"}, 5, nil)

	if _, err := sc.Run(ctx, ""); err != nil {
		t.Fatalf("scan: %v", err)
	}

	got, err := cat.GetVideo(ctx, "fake-drive-file-1")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	want := []string{"后入", "女大"}
	if !sameStrings(got.Tags, want) {
		t.Fatalf("tags = %#v, want %#v", got.Tags, want)
	}
}

type scannerFakeDrive struct {
	entries []drives.Entry
}

func (d *scannerFakeDrive) Kind() string { return "fake" }
func (d *scannerFakeDrive) ID() string   { return "drive" }
func (d *scannerFakeDrive) Init(context.Context) error {
	return nil
}
func (d *scannerFakeDrive) List(context.Context, string) ([]drives.Entry, error) {
	return d.entries, nil
}
func (d *scannerFakeDrive) Stat(context.Context, string) (*drives.Entry, error) {
	return nil, drives.ErrNotSupported
}
func (d *scannerFakeDrive) StreamURL(context.Context, string) (*drives.StreamLink, error) {
	return &drives.StreamLink{URL: "https://video.example/clip.mp4"}, nil
}
func (d *scannerFakeDrive) Upload(context.Context, string, string, io.Reader, int64) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *scannerFakeDrive) EnsureDir(context.Context, string) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *scannerFakeDrive) RootID() string { return "root" }
