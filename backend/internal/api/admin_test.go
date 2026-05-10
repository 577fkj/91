package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/video-site/backend/internal/catalog"
)

func TestHandleUpsertDrivePreservesExistingCredentialsWhenRequestCredentialsEmpty(t *testing.T) {
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

	if err := cat.UpsertDrive(ctx, &catalog.Drive{
		ID:         "quark-main",
		Kind:       "quark",
		Name:       "Old name",
		RootID:     "0",
		ScanRootID: "0",
		Credentials: map[string]string{
			"cookie": "existing-cookie",
		},
		Status: "ok",
	}); err != nil {
		t.Fatalf("seed drive: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/api/drives", strings.NewReader(`{
		"id": "quark-main",
		"kind": "quark",
		"name": "New name",
		"rootId": "0",
		"scanRootId": "scan-root",
		"credentials": {}
	}`))
	rr := httptest.NewRecorder()

	(&AdminServer{Catalog: cat}).handleUpsertDrive(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	got, err := cat.GetDrive(ctx, "quark-main")
	if err != nil {
		t.Fatalf("get drive: %v", err)
	}
	if got.Name != "New name" {
		t.Fatalf("name = %q, want New name", got.Name)
	}
	if got.ScanRootID != "scan-root" {
		t.Fatalf("scanRootId = %q, want scan-root", got.ScanRootID)
	}
	if got.Credentials["cookie"] != "existing-cookie" {
		t.Fatalf("cookie credential = %q, want existing-cookie", got.Credentials["cookie"])
	}
}

func TestHandleUpsertDriveReplacesExistingCredentialsWhenProvided(t *testing.T) {
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

	if err := cat.UpsertDrive(ctx, &catalog.Drive{
		ID:         "quark-main",
		Kind:       "quark",
		Name:       "Old name",
		RootID:     "0",
		ScanRootID: "0",
		Credentials: map[string]string{
			"cookie": "existing-cookie",
		},
		Status: "ok",
	}); err != nil {
		t.Fatalf("seed drive: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/api/drives", bytes.NewBufferString(`{
		"id": "quark-main",
		"kind": "quark",
		"name": "New name",
		"rootId": "0",
		"scanRootId": "0",
		"credentials": {"cookie": "new-cookie"}
	}`))
	rr := httptest.NewRecorder()

	(&AdminServer{Catalog: cat}).handleUpsertDrive(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	got, err := cat.GetDrive(ctx, "quark-main")
	if err != nil {
		t.Fatalf("get drive: %v", err)
	}
	if got.Credentials["cookie"] != "new-cookie" {
		t.Fatalf("cookie credential = %q, want new-cookie", got.Credentials["cookie"])
	}
}
