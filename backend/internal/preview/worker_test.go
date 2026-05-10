package preview

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/drives"
)

func TestThumbWorkerUpdatesThumbnailWithoutChangingPreviewStatus(t *testing.T) {
	ctx := context.Background()
	cat, video := seedPreviewTestVideo(t, "thumb-worker-video")

	gen := &fakeThumbGenerator{}
	drv := &previewFakeDrive{}
	worker := NewThumbWorker(gen, cat, drv)

	worker.process(ctx, video)

	got, err := cat.GetVideo(ctx, video.ID)
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if got.ThumbnailURL != "/p/thumb/"+video.ID {
		t.Fatalf("thumbnail = %q, want generated thumb URL", got.ThumbnailURL)
	}
	if got.PreviewStatus != "pending" {
		t.Fatalf("preview status = %q, want pending", got.PreviewStatus)
	}
	if got.DurationSeconds != 42 {
		t.Fatalf("duration = %d, want probed duration", got.DurationSeconds)
	}
	if gen.thumbnailVideoID != video.ID {
		t.Fatalf("thumbnail video id = %q, want %q", gen.thumbnailVideoID, video.ID)
	}
	if gen.thumbnailDuration != 42 {
		t.Fatalf("thumbnail duration = %.1f, want 42", gen.thumbnailDuration)
	}
	if drv.streamFileID != video.FileID {
		t.Fatalf("stream file id = %q, want %q", drv.streamFileID, video.FileID)
	}
}

func TestPreviewWorkerGeneratesTeaserWithoutReplacingExistingThumbnail(t *testing.T) {
	ctx := context.Background()
	cat, video := seedPreviewTestVideo(t, "preview-worker-video")
	video.ThumbnailURL = "https://thumbnail.example/original.jpg"
	if err := cat.UpsertVideo(ctx, video); err != nil {
		t.Fatalf("update video: %v", err)
	}

	gen := &fakeTeaserGenerator{}
	drv := &previewFakeDrive{}
	worker := NewWorker(gen, cat, drv, "")

	worker.process(ctx, video)

	got, err := cat.GetVideo(ctx, video.ID)
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if got.ThumbnailURL != "https://thumbnail.example/original.jpg" {
		t.Fatalf("thumbnail = %q, want existing thumbnail unchanged", got.ThumbnailURL)
	}
	if got.PreviewStatus != "ready" {
		t.Fatalf("preview status = %q, want ready", got.PreviewStatus)
	}
	if got.PreviewLocal != "/tmp/"+video.ID+".mp4" {
		t.Fatalf("preview local = %q, want moved teaser path", got.PreviewLocal)
	}
}

func seedPreviewTestVideo(t *testing.T, id string) (*catalog.Catalog, *catalog.Video) {
	t.Helper()
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

	video := &catalog.Video{
		ID:            id,
		DriveID:       "drive-id",
		FileID:        "file-id",
		Title:         "Clip",
		PreviewStatus: "pending",
		PublishedAt:   time.Now(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := cat.UpsertVideo(ctx, video); err != nil {
		t.Fatalf("seed video: %v", err)
	}
	return cat, video
}

type fakeThumbGenerator struct {
	thumbnailVideoID  string
	thumbnailDuration float64
}

func (g *fakeThumbGenerator) Probe(context.Context, *drives.StreamLink) (float64, error) {
	return 42, nil
}

func (g *fakeThumbGenerator) GenerateThumbnail(_ context.Context, _ *drives.StreamLink, videoID string, duration float64) (string, error) {
	g.thumbnailVideoID = videoID
	g.thumbnailDuration = duration
	return "/tmp/" + videoID + ".jpg", nil
}

type fakeTeaserGenerator struct{}

func (g *fakeTeaserGenerator) Probe(context.Context, *drives.StreamLink) (float64, error) {
	return 0, nil
}

func (g *fakeTeaserGenerator) Generate(context.Context, *drives.StreamLink, float64) (string, error) {
	return "/tmp/source-teaser.mp4", nil
}

func (g *fakeTeaserGenerator) MoveToLocal(_ string, videoID string) (string, error) {
	return "/tmp/" + videoID + ".mp4", nil
}

type previewFakeDrive struct {
	streamFileID string
}

func (d *previewFakeDrive) Kind() string { return "fake" }
func (d *previewFakeDrive) ID() string   { return "drive-id" }
func (d *previewFakeDrive) Init(context.Context) error {
	return nil
}
func (d *previewFakeDrive) List(context.Context, string) ([]drives.Entry, error) {
	return nil, nil
}
func (d *previewFakeDrive) Stat(context.Context, string) (*drives.Entry, error) {
	return nil, drives.ErrNotSupported
}
func (d *previewFakeDrive) StreamURL(_ context.Context, fileID string) (*drives.StreamLink, error) {
	d.streamFileID = fileID
	return &drives.StreamLink{URL: "https://video.example/clip.mp4"}, nil
}
func (d *previewFakeDrive) Upload(context.Context, string, string, io.Reader, int64) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *previewFakeDrive) EnsureDir(context.Context, string) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *previewFakeDrive) RootID() string { return "root" }
