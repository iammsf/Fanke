package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fanke/backend/internal/media"
	"fanke/backend/internal/repo"
)

func TestMediaContributionsContractAndGating(t *testing.T) {
	cfg := media.S3Config{Bucket: "photos", SecretKey: "not-returned"}
	image := repo.Extension{ID: 99, Slug: "image-processing", Enabled: true, Initialized: true, Hooks: []string{BeforeMediaUpload, AddSettingsItems}}
	storage := repo.Extension{ID: 1, Slug: "s3-storage", Enabled: true, Initialized: true, Hooks: []string{BeforeMediaUpload, AddSettingsItems, AddUploadMediaMenuItem}}
	got := capabilitiesFor([]repo.Extension{storage, image}, cfg)
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"settings":[{"id":"image-processing","plugin_slug":"image-processing","title":"图片处理","kind":"image-processing"},{"id":"s3-storage","plugin_slug":"s3-storage","title":"S3 对象存储","kind":"s3-storage"}],"upload_menu":[{"id":"s3-storage","plugin_slug":"s3-storage","label":"上传至photos","storage":"s3"}],"image_processing":true,"s3_storage":true}`
	if string(raw) != want {
		t.Fatalf("contract: %s", raw)
	}
	for _, state := range []string{"absent", "disabled", "uninitialized", "undeclared", "unknown"} {
		e := storage
		switch state {
		case "absent":
			e = repo.Extension{}
		case "disabled":
			e.Enabled = false
		case "uninitialized":
			e.Initialized = false
		case "undeclared":
			e.Hooks = nil
		case "unknown":
			e.Slug = "unregistered"
		}
		caps := capabilitiesFor([]repo.Extension{e}, cfg)
		if caps.S3Storage || len(caps.SettingsItems) != 0 || len(caps.UploadMenuItems) != 0 {
			t.Fatalf("%s exposed contributions: %+v", state, caps)
		}
	}
	image.Hooks = []string{AddSettingsItems}
	caps := capabilitiesFor([]repo.Extension{image}, cfg)
	if caps.ImageProcessing || len(caps.SettingsItems) != 1 {
		t.Fatal("declarations must be independent")
	}
	empty, _ := json.Marshal(capabilitiesFor(nil, cfg))
	if string(empty) != `{"settings":[],"upload_menu":[],"image_processing":false,"s3_storage":false}` {
		t.Fatalf("empty contract: %s", empty)
	}
}

func TestStorageSelectionRequiresPluginButLocalDoesNot(t *testing.T) {
	local, err := media.NewLocal(t.TempDir(), "/uploads")
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Local: local}
	for _, pref := range []string{"", "local"} {
		s, _, err := a.pickStorage(context.Background(), pref)
		if err != nil || s != local {
			t.Fatalf("%s: %v", pref, err)
		}
	}
	for _, pref := range []string{"s3", "r2", "unknown"} {
		s, _, err := a.pickStorage(context.Background(), pref)
		if err == nil || s != nil {
			t.Fatalf("invalid storage %s accepted", pref)
		}
	}
	artifacts, warning := a.prepareMediaArtifacts(context.Background(), &MediaUploadContext{Filename: "image.png"})
	if len(artifacts) != 0 || warning != "" {
		t.Fatalf("no-plugin processing: %v %s", artifacts, warning)
	}
}

func TestGenericSettingsRejectSensitiveKeys(t *testing.T) {
	for _, key := range []string{"s3_config", "r2_config"} {
		r := httptest.NewRequest(http.MethodPut, "/admin/settings?key="+key, strings.NewReader(`{"secret_key":"never-save"}`))
		w := httptest.NewRecorder()
		(&App{}).PutSetting(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d", key, w.Code)
		}
	}
}

func TestMaskS3Secrets(t *testing.T) {
	cfg := media.S3Config{SecretKey: "secret", SessionToken: "session", Bucket: "bucket"}
	got := maskSecret(cfg)
	if got.SecretKey != secretMask || got.SessionToken != secretMask || got.Bucket != "bucket" || cfg.SecretKey != "secret" {
		t.Fatal("secret masking changed config or leaked credentials")
	}
}
