package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/video-site/backend/internal/catalog"
)

func TestVideoSourceUsesTranscodeForAvi(t *testing.T) {
	v := &catalog.Video{
		ID:      "video-1",
		DriveID: "drive-1",
		FileID:  "file-1",
		Ext:     "avi",
	}

	got := videoSource(v)

	if got != "/p/transcode/video-1" {
		t.Fatalf("video source = %q, want transcode route", got)
	}
}

func TestVideoSourceKeepsDirectStreamForMp4(t *testing.T) {
	v := &catalog.Video{
		ID:      "video-1",
		DriveID: "drive-1",
		FileID:  "file-1",
		Ext:     "mp4",
	}

	got := videoSource(v)

	if got != "/p/stream/drive-1/file-1" {
		t.Fatalf("video source = %q, want direct stream route", got)
	}
}

func TestTranscodeStatusReadyWhenCachedFileExists(t *testing.T) {
	s := &Server{LocalDir: t.TempDir()}
	videoID := "video-1"
	path := s.transcodePath(videoID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("mp4"), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	if got := s.transcodeStatus(videoID); got != "ready" {
		t.Fatalf("status = %q, want ready", got)
	}
}

func TestTranscodeStatusProcessingWhenJobActive(t *testing.T) {
	s := &Server{LocalDir: t.TempDir()}
	videoID := "video-1"
	s.setTranscoding(videoID, true)

	if got := s.transcodeStatus(videoID); got != "processing" {
		t.Fatalf("status = %q, want processing", got)
	}
}

func TestTranscodeTempPathKeepsMp4Extension(t *testing.T) {
	s := &Server{LocalDir: t.TempDir()}

	if got := s.transcodeTempPath("video-1"); !strings.HasSuffix(got, ".mp4") {
		t.Fatalf("temp transcode path = %q, want .mp4 suffix for ffmpeg muxer detection", got)
	}
}

func TestHandleTagsReturnsFixedTagsOnly(t *testing.T) {
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
		ID:          "video-1",
		DriveID:     "drive",
		FileID:      "file-1",
		Title:       "女大后入",
		Tags:        []string{"后入", "女大", "sunny"},
		Category:    "random-category",
		PublishedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	rr := httptest.NewRecorder()
	(&Server{Catalog: cat}).handleTags(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Count int    `json:"count"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	labels := make([]string, 0, len(got))
	for _, tag := range got {
		labels = append(labels, tag.Label)
	}
	want := []string{"后入", "奶子", "口交", "臀", "人妻", "女大"}
	if !sameStrings(labels, want) {
		t.Fatalf("labels = %#v, want %#v", labels, want)
	}
	if got[0].Count != 1 || got[5].Count != 1 {
		t.Fatalf("counts = %#v, want 后入 and 女大 count 1", got)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
