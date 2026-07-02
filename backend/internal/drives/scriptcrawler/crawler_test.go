package scriptcrawler

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/fingerprint"
	"github.com/video-site/backend/internal/mediaasset"
)

const (
	scriptCrawlerDuplicateBytes = "duplicate-video-bytes"
	scriptCrawlerUniqueBytes    = "unique-video-bytes"
)

func init() {
	base := strings.ToLower(filepath.Base(os.Args[0]))
	switch {
	case strings.HasPrefix(base, "scriptcrawler-helper"):
		runScriptCrawlerHelperProcess()
		os.Exit(0)
	case strings.HasPrefix(base, "dryrun-helper"):
		runDryRunHelperProcess()
		os.Exit(0)
	case strings.HasPrefix(base, "ffprobe-ok"):
		fmt.Println("video")
		os.Exit(0)
	case strings.HasPrefix(base, "ffprobe-fail"):
		fmt.Fprintln(os.Stderr, "moov atom not found")
		os.Exit(1)
	case strings.HasPrefix(base, "ffmpeg-hls"):
		if len(os.Args) == 0 {
			os.Exit(1)
		}
		if argsFile := os.Getenv("GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE"); argsFile != "" {
			if err := os.WriteFile(argsFile, []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		out := os.Args[len(os.Args)-1]
		if err := os.WriteFile(out, []byte("hls-video-bytes"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func copyTestExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	target := filepath.Join(dir, name+filepath.Ext(exe))
	src, err := os.Open(exe)
	if err != nil {
		t.Fatalf("open test executable: %v", err)
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create helper executable: %v", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		t.Fatalf("copy helper executable: %v", err)
	}
	if err := dst.Close(); err != nil {
		t.Fatalf("close helper executable: %v", err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatalf("chmod helper executable: %v", err)
	}
	return target
}

func scriptCrawlerHelperExecutable(t *testing.T, dir string) string {
	t.Helper()
	return copyTestExecutable(t, dir, "scriptcrawler-helper")
}

func dryRunHelperExecutable(t *testing.T) string {
	t.Helper()
	return copyTestExecutable(t, t.TempDir(), "dryrun-helper")
}

func writeScriptCrawlerFFprobeStub(t *testing.T, dir string, ok bool) string {
	t.Helper()
	if ok {
		return copyTestExecutable(t, dir, "ffprobe-ok")
	}
	return copyTestExecutable(t, dir, "ffprobe-fail")
}

func writeScriptCrawlerFFmpegStub(t *testing.T, dir string) string {
	t.Helper()
	return copyTestExecutable(t, dir, "ffmpeg-hls")
}

func writeScriptCrawlerJPEG(t *testing.T, path string, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jpeg: %v", err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
}

func TestCrawlerRunOnceImportsLocalFileAndSkipsExisting(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	dummyScript := filepath.Join(tmp, "helper-script")
	if err := os.WriteFile(dummyScript, []byte("helper"), 0o755); err != nil {
		t.Fatalf("write dummy script: %v", err)
	}
	wrapper := scriptCrawlerHelperExecutable(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		CrawlerName: "Demo Crawler",
		PythonPath:  wrapper,
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  dummyScript,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("result = new:%d skipped:%d failed:%d, want 1/0/0", res.NewVideos, res.Skipped, res.Failed)
	}
	v, err := cat.GetVideo(ctx, BuildVideoID("demo", "abc-123"))
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if v.Title != "Imported From Helper" || v.FileID != "abc-123.mp4" || v.Size == 0 {
		t.Fatalf("video = title:%q file:%q size:%d", v.Title, v.FileID, v.Size)
	}
	if !hasString(v.Tags, "Demo Crawler") {
		t.Fatalf("video tags = %#v, want crawler name tag", v.Tags)
	}
	if _, err := os.Stat(filepath.Join(drv.VideosDir(), "abc-123.mp4")); err != nil {
		t.Fatalf("video file not copied: %v", err)
	}

	res, err = c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.NewVideos != 0 || res.Skipped != 1 {
		t.Fatalf("second result = new:%d skipped:%d, want 0/1", res.NewVideos, res.Skipped)
	}
	if res.SeenSnapshot != 1 {
		t.Fatalf("seen snapshot = %d, want 1", res.SeenSnapshot)
	}
}

func TestCrawlerRunOnceMarksPreviewDisabledWhenConfigured(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	dummyScript := filepath.Join(tmp, "helper-script")
	if err := os.WriteFile(dummyScript, []byte("helper"), 0o755); err != nil {
		t.Fatalf("write dummy script: %v", err)
	}
	wrapper := scriptCrawlerHelperExecutable(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:         drv,
		Catalog:        cat,
		PythonPath:     wrapper,
		FFprobePath:    writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:     dummyScript,
		DisablePreview: true,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Failed != 0 {
		t.Fatalf("result = new:%d failed:%d, want 1/0", res.NewVideos, res.Failed)
	}
	v, err := cat.GetVideo(ctx, BuildVideoID("demo", "abc-123"))
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if v.PreviewStatus != "disabled" {
		t.Fatalf("preview status = %q, want disabled", v.PreviewStatus)
	}
	if v.FingerprintStatus != "ready" || v.SampledSHA256 == "" {
		t.Fatalf("fingerprint status=%q sampled=%q, want ready and sampled hash", v.FingerprintStatus, v.SampledSHA256)
	}
	pending, err := cat.ListVideosByPreviewStatus(ctx, "demo", "pending", 0)
	if err != nil {
		t.Fatalf("list pending previews: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending previews = %d, want 0", len(pending))
	}
}

func TestCrawlerRunOnceUsesCurrentDrivePreviewSwitch(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	if err := cat.UpsertDrive(ctx, &catalog.Drive{
		ID:            drv.ID(),
		Kind:          Kind,
		Name:          "Demo",
		RootID:        "/",
		Credentials:   map[string]string{"script_path": "/tmp/crawler.py"},
		TeaserEnabled: true,
	}); err != nil {
		t.Fatalf("seed drive: %v", err)
	}
	dummyScript := filepath.Join(tmp, "helper-script")
	if err := os.WriteFile(dummyScript, []byte("helper"), 0o755); err != nil {
		t.Fatalf("write dummy script: %v", err)
	}
	wrapper := scriptCrawlerHelperExecutable(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:         drv,
		Catalog:        cat,
		PythonPath:     wrapper,
		FFprobePath:    writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:     dummyScript,
		DisablePreview: true,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Failed != 0 {
		t.Fatalf("result = new:%d failed:%d, want 1/0", res.NewVideos, res.Failed)
	}
	v, err := cat.GetVideo(ctx, BuildVideoID("demo", "abc-123"))
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if v.PreviewStatus != "pending" {
		t.Fatalf("preview status = %q, want pending from current drive switch", v.PreviewStatus)
	}
}

func TestCrawlerRunOnceUsesDefaultCrawlerNamespace(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	dummyScript := filepath.Join(tmp, "helper-script")
	if err := os.WriteFile(dummyScript, []byte("helper"), 0o755); err != nil {
		t.Fatalf("write dummy script: %v", err)
	}
	wrapper := scriptCrawlerHelperExecutable(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		PythonPath:  wrapper,
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  dummyScript,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.SeenSnapshot != 0 {
		t.Fatalf("result = new:%d seen:%d, want 1/0", res.NewVideos, res.SeenSnapshot)
	}
	videoID := BuildVideoID("demo", "abc-123")
	if _, err := cat.GetVideo(ctx, videoID); err != nil {
		t.Fatalf("get crawler video: %v", err)
	}

	res, err = c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.NewVideos != 0 || res.Skipped != 1 || res.SeenSnapshot != 1 {
		t.Fatalf("second result = new:%d skipped:%d seen:%d, want 0/1/1", res.NewVideos, res.Skipped, res.SeenSnapshot)
	}
}

func TestCrawlerRunOncePassesAbsoluteJobPathsWhenWorkDirDiffers(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join("data", "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	scriptDir := filepath.Join(tmp, "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	dummyScript := filepath.Join(scriptDir, "helper-script")
	if err := os.WriteFile(dummyScript, []byte("helper"), 0o755); err != nil {
		t.Fatalf("write dummy script: %v", err)
	}
	wrapper := scriptCrawlerHelperExecutable(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	t.Setenv("GO_WANT_SCRIPTCRAWLER_ASSERT_ABS", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		PythonPath:  wrapper,
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  dummyScript,
		WorkDir:     scriptDir,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("result = new:%d skipped:%d failed:%d, want 1/0/0", res.NewVideos, res.Skipped, res.Failed)
	}
	if !filepath.IsAbs(res.JobFile) || !filepath.IsAbs(res.SeenFile) {
		t.Fatalf("result paths should be absolute: job=%q seen=%q", res.JobFile, res.SeenFile)
	}
}

func TestCrawlerRunOnceImportsSimpleMediaURLWithoutSourceID(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/video.mp4" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("simple-video-bytes"))
	}))
	defer srv.Close()

	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	dummyScript := filepath.Join(tmp, "helper-script")
	if err := os.WriteFile(dummyScript, []byte("helper"), 0o755); err != nil {
		t.Fatalf("write dummy script: %v", err)
	}
	wrapper := scriptCrawlerHelperExecutable(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	t.Setenv("GO_WANT_SCRIPTCRAWLER_SIMPLE", "1")
	t.Setenv("GO_SCRIPTCRAWLER_MEDIA_URL", srv.URL+"/video.mp4?token=first")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		PythonPath:  wrapper,
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  dummyScript,
		HTTPClient:  srv.Client(),
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("result = new:%d skipped:%d failed:%d, want 1/0/0", res.NewVideos, res.Skipped, res.Failed)
	}
	videos, err := cat.ListVideosByDrive(ctx, "demo")
	if err != nil {
		t.Fatalf("list videos: %v", err)
	}
	if len(videos) != 1 {
		t.Fatalf("videos = %d, want 1", len(videos))
	}
	v := videos[0]
	if !strings.HasPrefix(v.ID, BuildVideoID("demo", "auto-")) {
		t.Fatalf("video id = %q, want generated auto source id", v.ID)
	}
	if v.Title != "Simple Protocol Video" || v.Ext != "mp4" || v.ThumbnailURL != "" || v.Size == 0 {
		t.Fatalf("video = title:%q ext:%q thumb:%q size:%d", v.Title, v.Ext, v.ThumbnailURL, v.Size)
	}
	if _, err := os.Stat(filepath.Join(drv.VideosDir(), v.FileID)); err != nil {
		t.Fatalf("video file not downloaded: %v", err)
	}

	t.Setenv("GO_SCRIPTCRAWLER_MEDIA_URL", srv.URL+"/video.mp4?token=second")
	res, err = c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.NewVideos != 0 || res.Skipped != 1 {
		t.Fatalf("second result = new:%d skipped:%d, want 0/1", res.NewVideos, res.Skipped)
	}
}

func TestCrawlerRunOnceSkipsFingerprintDuplicateAndContinues(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}

	seedFile := "seed-canonical.mp4"
	if err := os.WriteFile(filepath.Join(drv.VideosDir(), seedFile), []byte(scriptCrawlerDuplicateBytes), 0o644); err != nil {
		t.Fatalf("write seed video: %v", err)
	}
	seed := &catalog.Video{
		ID:          "seed-for-hash",
		DriveID:     drv.ID(),
		FileID:      seedFile,
		Title:       "Seed",
		Size:        int64(len(scriptCrawlerDuplicateBytes)),
		PublishedAt: time.Now(),
	}
	sampled, err := fingerprint.Compute(ctx, drv, seed, fingerprint.Config{}, nil)
	if err != nil {
		t.Fatalf("compute seed fingerprint: %v", err)
	}
	_ = os.Remove(filepath.Join(drv.VideosDir(), seedFile))

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID:                "existing-canonical",
		DriveID:           "other-drive",
		FileID:            "existing.mp4",
		FileName:          "existing.mp4",
		Title:             "Existing Canonical",
		Size:              int64(len(scriptCrawlerDuplicateBytes)),
		Ext:               "mp4",
		SampledSHA256:     sampled,
		FingerprintStatus: "ready",
		PublishedAt:       now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}); err != nil {
		t.Fatalf("seed canonical video: %v", err)
	}

	dummyScript := filepath.Join(tmp, "helper-script")
	if err := os.WriteFile(dummyScript, []byte("helper"), 0o755); err != nil {
		t.Fatalf("write dummy script: %v", err)
	}
	wrapper := scriptCrawlerHelperExecutable(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	t.Setenv("GO_WANT_SCRIPTCRAWLER_DUP_UNIQUE", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		PythonPath:  wrapper,
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  dummyScript,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Skipped != 1 || res.Failed != 0 || res.TotalEntries != 2 {
		t.Fatalf("result = total:%d new:%d skipped:%d failed:%d, want 2/1/1/0", res.TotalEntries, res.NewVideos, res.Skipped, res.Failed)
	}
	if res.CandidateBudget <= res.TargetNew {
		t.Fatalf("candidate budget = %d, target = %d; want expanded budget", res.CandidateBudget, res.TargetNew)
	}
	if _, err := cat.GetVideo(ctx, BuildVideoID("demo", "dup-source")); err == nil {
		t.Fatal("duplicate candidate should not be imported")
	}
	if _, err := os.Stat(filepath.Join(drv.VideosDir(), "dup-source.mp4")); !os.IsNotExist(err) {
		t.Fatalf("duplicate local file stat = %v, want removed", err)
	}
	v, err := cat.GetVideo(ctx, BuildVideoID("demo", "unique-source"))
	if err != nil {
		t.Fatalf("unique video should be imported: %v", err)
	}
	if v.SampledSHA256 == "" || v.FingerprintStatus != "ready" {
		t.Fatalf("unique fingerprint = %q status=%q, want ready sampled fingerprint", v.SampledSHA256, v.FingerprintStatus)
	}
	seen, err := cat.ListCrawlerSourceIDs(ctx, Kind, "demo")
	if err != nil {
		t.Fatalf("list seen source ids: %v", err)
	}
	seenSet := map[string]bool{}
	for _, id := range seen {
		seenSet[id] = true
	}
	if !seenSet["dup-source"] || !seenSet["unique-source"] {
		t.Fatalf("seen ids = %#v, want duplicate and imported source ids", seen)
	}
}

func TestCrawlerProcessItemSkipsNearDuplicateByTitleDurationAndThumbnail(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	commonThumbDir := filepath.Join(tmp, "common-thumbs")
	if err := os.MkdirAll(commonThumbDir, 0o755); err != nil {
		t.Fatalf("mkdir common thumbs: %v", err)
	}

	now := time.Now()
	canonicalID := "existing-canonical"
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID:              canonicalID,
		DriveID:         "other-drive",
		FileID:          "existing.mp4",
		FileName:        "existing.mp4",
		Title:           "91 Test Similar Title 1215516",
		DurationSeconds: 257,
		Size:            12345,
		Ext:             "mp4",
		ThumbnailURL:    "/p/thumb/" + canonicalID,
		PublishedAt:     now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("seed canonical video: %v", err)
	}
	writeScriptCrawlerJPEG(t, mediaasset.ThumbnailPathInDir(commonThumbDir, canonicalID), color.RGBA{R: 210, G: 40, B: 40, A: 255})

	outputDir := drv.OutputDir()
	mediaPath := filepath.Join(outputDir, "near-video.mp4")
	if err := os.WriteFile(mediaPath, []byte("near-duplicate-but-different-bytes"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}
	thumbPath := filepath.Join(outputDir, "near-thumb.jpg")
	writeScriptCrawlerJPEG(t, thumbPath, color.RGBA{R: 211, G: 41, B: 41, A: 255})

	c := NewCrawler(CrawlerConfig{
		Driver:         drv,
		Catalog:        cat,
		FFprobePath:    writeScriptCrawlerFFprobeStub(t, tmp, true),
		CommonThumbDir: commonThumbDir,
	})
	imported, err := c.processItem(ctx, Item{
		SourceID:        "near-source",
		Title:           "91 Test Similar Title 1215516 - source suffix",
		Author:          "helper",
		DurationSeconds: 257,
		Media:           MediaRef{LocalFile: mediaPath},
		Thumbnail:       MediaRef{LocalFile: thumbPath},
	})
	if err != nil {
		t.Fatalf("process item: %v", err)
	}
	if imported {
		t.Fatal("near duplicate imported, want skipped")
	}
	if _, err := cat.GetVideo(ctx, BuildVideoID("demo", "near-source")); err == nil {
		t.Fatal("near duplicate should not be inserted into catalog")
	}
	if _, err := os.Stat(filepath.Join(drv.VideosDir(), "near-source.mp4")); !os.IsNotExist(err) {
		t.Fatalf("near duplicate video stat = %v, want removed", err)
	}
	if sourceThumb, err := drv.ThumbPath("near-source.jpg"); err != nil {
		t.Fatalf("source thumb path: %v", err)
	} else if _, err := os.Stat(sourceThumb); !os.IsNotExist(err) {
		t.Fatalf("source thumb stat = %v, want removed", err)
	}
	if _, err := os.Stat(mediaasset.ThumbnailPathInDir(commonThumbDir, BuildVideoID("demo", "near-source"))); !os.IsNotExist(err) {
		t.Fatalf("common thumb stat = %v, want removed", err)
	}
	seen, err := cat.ListCrawlerSourceIDs(ctx, Kind, "demo")
	if err != nil {
		t.Fatalf("list seen source ids: %v", err)
	}
	if !hasString(seen, "near-source") {
		t.Fatalf("seen ids = %#v, want near-source", seen)
	}
}

func TestCrawlerProcessItemKeepsLargerNearDuplicate(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	commonThumbDir := filepath.Join(tmp, "common-thumbs")
	if err := os.MkdirAll(commonThumbDir, 0o755); err != nil {
		t.Fatalf("mkdir common thumbs: %v", err)
	}

	now := time.Now()
	smallerID := "smaller-canonical"
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID:              smallerID,
		DriveID:         "other-drive",
		FileID:          "smaller.mp4",
		FileName:        "smaller.mp4",
		Title:           "91 Test Larger Candidate 1215516",
		DurationSeconds: 257,
		Size:            5,
		Ext:             "mp4",
		ThumbnailURL:    "/p/thumb/" + smallerID,
		PublishedAt:     now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("seed smaller video: %v", err)
	}
	writeScriptCrawlerJPEG(t, mediaasset.ThumbnailPathInDir(commonThumbDir, smallerID), color.RGBA{R: 80, G: 160, B: 80, A: 255})

	outputDir := drv.OutputDir()
	mediaPath := filepath.Join(outputDir, "larger-video.mp4")
	if err := os.WriteFile(mediaPath, []byte("near-duplicate-larger-candidate-bytes"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}
	thumbPath := filepath.Join(outputDir, "larger-thumb.jpg")
	writeScriptCrawlerJPEG(t, thumbPath, color.RGBA{R: 81, G: 161, B: 81, A: 255})

	c := NewCrawler(CrawlerConfig{
		Driver:         drv,
		Catalog:        cat,
		FFprobePath:    writeScriptCrawlerFFprobeStub(t, tmp, true),
		CommonThumbDir: commonThumbDir,
	})
	imported, err := c.processItem(ctx, Item{
		SourceID:        "larger-source",
		Title:           "91 Test Larger Candidate 1215516 - source suffix",
		Author:          "helper",
		DurationSeconds: 257,
		Media:           MediaRef{LocalFile: mediaPath},
		Thumbnail:       MediaRef{LocalFile: thumbPath},
	})
	if err != nil {
		t.Fatalf("process item: %v", err)
	}
	if !imported {
		t.Fatal("larger near duplicate was skipped, want imported")
	}
	if _, err := cat.GetVideo(ctx, smallerID); err == nil {
		t.Fatal("smaller near duplicate should be deleted from catalog")
	}
	if deleted, err := cat.IsVideoDeleted(ctx, smallerID); err != nil || !deleted {
		t.Fatalf("smaller tombstone = %v, %v; want deleted tombstone", deleted, err)
	}
	larger, err := cat.GetVideo(ctx, BuildVideoID("demo", "larger-source"))
	if err != nil {
		t.Fatalf("larger video should be imported: %v", err)
	}
	if larger.Size <= 5 {
		t.Fatalf("larger size = %d, want > 5", larger.Size)
	}
}

func TestCrawlerRunOnceRejectsInvalidDownloadedVideo(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	dummyScript := filepath.Join(tmp, "helper-script")
	if err := os.WriteFile(dummyScript, []byte("helper"), 0o755); err != nil {
		t.Fatalf("write dummy script: %v", err)
	}
	wrapper := scriptCrawlerHelperExecutable(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		CrawlerName: "Demo Crawler",
		PythonPath:  wrapper,
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, false),
		ScriptPath:  dummyScript,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 0 || res.Skipped != 0 || res.Failed != 1 || res.TotalEntries != 1 {
		t.Fatalf("result = total:%d new:%d skipped:%d failed:%d, want 1/0/0/1", res.TotalEntries, res.NewVideos, res.Skipped, res.Failed)
	}
	if _, err := cat.GetVideo(ctx, BuildVideoID("demo", "abc-123")); err == nil {
		t.Fatal("invalid video should not be imported")
	}
	if _, err := os.Stat(filepath.Join(drv.VideosDir(), "abc-123.mp4")); !os.IsNotExist(err) {
		t.Fatalf("invalid local video stat = %v, want removed", err)
	}
	seen, err := cat.ListCrawlerSourceIDs(ctx, Kind, "demo")
	if err != nil {
		t.Fatalf("list seen source ids: %v", err)
	}
	if len(seen) != 0 {
		t.Fatalf("seen ids = %#v, want none for invalid video", seen)
	}
}

func TestCrawlerRunOnceDownloadsHLSMediaURL(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	dummyScript := filepath.Join(tmp, "helper-script")
	if err := os.WriteFile(dummyScript, []byte("helper"), 0o755); err != nil {
		t.Fatalf("write dummy script: %v", err)
	}
	wrapper := scriptCrawlerHelperExecutable(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	t.Setenv("GO_WANT_SCRIPTCRAWLER_HLS", "1")
	ffmpegArgsFile := filepath.Join(tmp, "ffmpeg-args.txt")
	t.Setenv("GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE", ffmpegArgsFile)
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		CrawlerName: "Demo Crawler",
		PythonPath:  wrapper,
		FFmpegPath:  writeScriptCrawlerFFmpegStub(t, tmp),
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  dummyScript,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("result = new:%d skipped:%d failed:%d, want 1/0/0", res.NewVideos, res.Skipped, res.Failed)
	}
	v, err := cat.GetVideo(ctx, BuildVideoID("demo", "hls-source"))
	if err != nil {
		t.Fatalf("get hls video: %v", err)
	}
	if v.FileID != "hls-source.mp4" || v.Size != int64(len("hls-video-bytes")) {
		t.Fatalf("video file=%q size=%d, want hls-source.mp4 size %d", v.FileID, v.Size, len("hls-video-bytes"))
	}
	data, err := os.ReadFile(filepath.Join(drv.VideosDir(), "hls-source.mp4"))
	if err != nil {
		t.Fatalf("read hls output: %v", err)
	}
	if string(data) != "hls-video-bytes" {
		t.Fatalf("hls output = %q", string(data))
	}
	argsData, err := os.ReadFile(ffmpegArgsFile)
	if err != nil {
		t.Fatalf("read ffmpeg args: %v", err)
	}
	argsText := "\n" + string(argsData) + "\n"
	for _, want := range []string{
		"\n-protocol_whitelist\nhttp,https,tcp,tls,crypto\n",
		"\n-allowed_extensions\nALL\n",
		"\n-allowed_segment_extensions\nALL\n",
		"\n-extension_picky\n0\n",
	} {
		if !strings.Contains(argsText, want) {
			t.Fatalf("ffmpeg args missing %q in:\n%s", strings.TrimSpace(want), string(argsData))
		}
	}
}

func runScriptCrawlerHelperProcess() {
	args := os.Args
	jobPath := ""
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--job" {
			jobPath = args[i+1]
			break
		}
	}
	if jobPath == "" {
		fmt.Fprintln(os.Stderr, "missing --job")
		os.Exit(2)
	}
	data, err := os.ReadFile(jobPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if os.Getenv("GO_WANT_SCRIPTCRAWLER_ASSERT_ABS") == "1" {
		if !filepath.IsAbs(jobPath) || !filepath.IsAbs(job.SeenSourceIDsFile) || !filepath.IsAbs(job.OutputDir) {
			fmt.Fprintf(os.Stderr, "expected absolute paths, got job=%q seen=%q output=%q\n", jobPath, job.SeenSourceIDsFile, job.OutputDir)
			os.Exit(2)
		}
	}
	if os.Getenv("GO_WANT_SCRIPTCRAWLER_SIMPLE") == "1" {
		event := map[string]any{
			"title":     "Simple Protocol Video",
			"media_url": os.Getenv("GO_SCRIPTCRAWLER_MEDIA_URL"),
		}
		_ = json.NewEncoder(os.Stdout).Encode(event)
		os.Exit(0)
	}
	if os.Getenv("GO_WANT_SCRIPTCRAWLER_HLS") == "1" {
		event := Event{
			Type: "item",
			Item: Item{
				SourceID: "hls-source",
				Title:    "HLS Protocol Video",
				Author:   "helper",
				Media: MediaRef{
					URL: "https://media.example.test/video.m3u8",
					Headers: map[string]string{
						"Referer": "https://example.test/",
					},
				},
			},
		}
		_ = json.NewEncoder(os.Stdout).Encode(event)
		os.Exit(0)
	}
	if os.Getenv("GO_WANT_SCRIPTCRAWLER_DUP_UNIQUE") == "1" {
		duplicateFile := filepath.Join(job.OutputDir, "duplicate.mp4")
		if err := os.WriteFile(duplicateFile, []byte(scriptCrawlerDuplicateBytes), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		uniqueFile := filepath.Join(job.OutputDir, "unique.mp4")
		if err := os.WriteFile(uniqueFile, []byte(scriptCrawlerUniqueBytes), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		for _, event := range []Event{
			{
				Type: "item",
				Item: Item{
					SourceID: "dup-source",
					Title:    "Duplicate Candidate",
					Author:   "helper",
					Media:    MediaRef{LocalFile: duplicateFile},
				},
			},
			{
				Type: "item",
				Item: Item{
					SourceID: "unique-source",
					Title:    "Unique Candidate",
					Author:   "helper",
					Media:    MediaRef{LocalFile: uniqueFile},
				},
			},
		} {
			_ = json.NewEncoder(os.Stdout).Encode(event)
		}
		os.Exit(0)
	}
	localFile := filepath.Join(job.OutputDir, "helper.mp4")
	if err := os.WriteFile(localFile, []byte("helper-video"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	event := Event{
		Type: "item",
		Item: Item{
			SourceID: "abc-123",
			Title:    "Imported From Helper",
			Author:   "helper",
			Media:    MediaRef{LocalFile: localFile},
		},
	}
	_ = json.NewEncoder(os.Stdout).Encode(event)
}

func runDryRunHelperProcess() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "missing script path")
		os.Exit(2)
	}
	bodyBytes, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	body := string(bodyBytes)
	switch {
	case strings.Contains(body, "Early Stop Video"):
		fmt.Fprintln(os.Stderr, "[log] first item ready")
		fmt.Println(`{"type":"item","item":{"title":"Early Stop Video","media_url":"https://cdn.example.test/v.mp4","source_id":"early-stop"}}`)
		time.Sleep(30 * time.Second)
	case strings.Contains(body, "sleep 30"):
		time.Sleep(30 * time.Second)
	case strings.Contains(body, "plain text progress output"):
		fmt.Println("plain text progress output")
	case strings.Contains(body, "[log] fetching list page"):
		fmt.Fprintln(os.Stderr, "[log] fetching list page")
		fmt.Println(`{"type":"item","item":{"title":"Test Video","media_url":"https://cdn.example.test/v.mp4","source_id":"123","thumbnail_url":"https://cdn.example.test/t.jpg"}}`)
		fmt.Println(`{"type":"done","stats":{"emitted":1}}`)
	case strings.Contains(body, "Probe Video"):
		mediaURL := dryRunMediaURLFromBody(body)
		fmt.Printf("{\"type\":\"item\",\"title\":\"Probe Video\",\"media_url\":%q,\"detail_url\":\"https://example.test/view\"}\n", mediaURL)
	case strings.Contains(body, "Dead Link"):
		mediaURL := dryRunMediaURLFromBody(body)
		fmt.Printf("{\"type\":\"item\",\"title\":\"Dead Link\",\"media_url\":%q}\n", mediaURL)
	default:
		fmt.Println(strings.TrimSpace(body))
	}
}

func dryRunMediaURLFromBody(body string) string {
	const marker = `"media_url":"`
	start := strings.Index(body, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		return body[start:]
	}
	return body[start : start+end]
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
