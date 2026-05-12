package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/storageusage"
)

func (a *AdminServer) handleDriveStorage(w http.ResponseWriter, r *http.Request) {
	usage, err := collectLocalMediaStorage(r.Context(), a.Catalog, a.LocalPreviewDir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func collectLocalMediaStorage(ctx context.Context, cat *catalog.Catalog, localDir string) (storageusage.Usage, error) {
	if cat == nil {
		return storageusage.Usage{}, errors.New("catalog is not configured")
	}
	localDir = strings.TrimSpace(localDir)
	if localDir == "" {
		return storageusage.Usage{}, errors.New("local preview dir is not configured")
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return storageusage.Usage{}, err
	}
	drives, err := cat.ListDrives(ctx)
	if err != nil {
		return storageusage.Usage{}, err
	}
	refs, err := cat.ListLocalMediaRefs(ctx)
	if err != nil {
		return storageusage.Usage{}, err
	}
	driveIDs := make([]string, 0, len(drives))
	for _, drive := range drives {
		driveIDs = append(driveIDs, drive.ID)
	}
	assetRefs := make([]storageusage.VideoAssetRef, 0, len(refs))
	for _, ref := range refs {
		assetRefs = append(assetRefs, storageusage.VideoAssetRef{
			ID:           ref.VideoID,
			DriveID:      ref.DriveID,
			PreviewLocal: ref.PreviewLocal,
		})
	}
	return storageusage.Compute(localDir, assetRefs, driveIDs, localDiskStats)
}

func localDiskStats(path string) (storageusage.DiskStats, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return storageusage.DiskStats{}, err
	}
	blockSize := uint64(stat.Bsize)
	return storageusage.DiskStats{
		AvailableBytes: int64(uint64(stat.Bavail) * blockSize),
		CapacityBytes:  int64(uint64(stat.Blocks) * blockSize),
	}, nil
}
