package app

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"fanke/backend/internal/media"
	"fanke/backend/internal/repo"
)

func TestLegacyBucketMigrationAndCredentials(t *testing.T) {
	pathStyle := false
	old := media.S3Config{Endpoint: "https://s3.example", Region: "region", Bucket: "photos", PublicBase: "https://img.example", AccessKey: "access", SecretKey: "secret", SessionToken: "token", ForcePathStyle: &pathStyle}
	first, second := repo.MigrateS3Config(old), repo.MigrateS3Config(old)
	if !reflect.DeepEqual(first, second) || first.DefaultBucketID == "" || first.Buckets[0].Alias != "photos" {
		t.Fatal("unstable migration", first)
	}
	migrated := first.Buckets[0]
	if migrated.AccessKey != old.AccessKey || migrated.SecretKey != old.SecretKey || migrated.SessionToken != old.SessionToken || *migrated.ForcePathStyle {
		t.Fatal("migration lost credentials")
	}
	updated := preserveBucketCredentials(media.S3Config{ID: migrated.ID, SecretKey: secretMask, SessionToken: secretMask}, migrated)
	if updated.AccessKey != old.AccessKey || updated.SecretKey != old.SecretKey || updated.SessionToken != old.SessionToken || updated.ForcePathStyle == nil {
		t.Fatal("masked update lost credentials")
	}
	rotated := preserveBucketCredentials(media.S3Config{SecretKey: "new", SessionToken: "new-token"}, migrated)
	if rotated.SecretKey != "new" || rotated.SessionToken != "new-token" {
		t.Fatal("rotation lost")
	}
}

func TestBucketSelectorsAndHistoricalURLs(t *testing.T) {
	bs := []media.S3Config{{ID: "a", PublicBase: "https://img.example"}, {ID: "b", PublicBase: "https://img.example/history", Enabled: false}}
	settings := repo.S3Settings{Buckets: bs, DefaultBucketID: "b"}
	for _, tc := range []struct {
		id, storage, want string
		bad               bool
	}{
		{"", "", "b", false}, {"a", "", "a", false}, {"", "s3:a", "a", false}, {"b", "s3:b", "b", false}, {"a", "s3:b", "", true}, {"", "local", "", true}, {"", "s3:", "", true}, {"unknown", "", "", true},
	} {
		id, err := syncBucketID(tc.id, tc.storage)
		var c media.S3Config
		if err == nil {
			c, err = settings.SelectBucket(id)
		}
		if (err != nil) != tc.bad || (!tc.bad && c.ID != tc.want) {
			t.Fatalf("%+v => %s %v", tc, c.ID, err)
		}
	}
	cfg, key, ok := bucketForURL(bs, "https://img.example/history/a.jpg")
	if !ok || cfg.ID != "b" || key != "a.jpg" {
		t.Fatal("historical/longest URL not selected", cfg, key)
	}
	if _, _, ok := bucketForURL(bs, "https://img.example.evil/a.jpg"); ok {
		t.Fatal("host boundary bypass")
	}
	for _, storage := range []string{"s3", "s3:b"} {
		if !recordInBucket(storage, "https://img.example/history/a.jpg", "a.jpg", bs[1]) {
			t.Fatal("selected bucket missing")
		}
		if recordInBucket(storage, "https://img.example/history/a.jpg", "a.jpg", bs[0]) {
			t.Fatal("other bucket classified as missing/protected")
		}
	}
	if recordInBucket("s3:a", "https://img.example/history/a.jpg", "a.jpg", bs[1]) {
		t.Fatal("explicit bucket mismatch")
	}
}

func TestMultipleBucketMenuAliasAndDefault(t *testing.T) {
	ext := repo.Extension{Slug: "s3-storage", Enabled: true, Initialized: true, Hooks: []string{BeforeMediaUpload, AddUploadMediaMenuItem}}
	caps := capabilitiesForBuckets([]repo.Extension{ext}, []media.S3Config{{ID: "a", Alias: "图库"}, {ID: "b", Alias: "附件"}}, "b")
	if len(caps.UploadMenuItems) != 2 {
		t.Fatal(caps)
	}
	for i, want := range []string{"上传至图库", "上传至附件"} {
		item := caps.UploadMenuItems[i]
		if item.Label != want || item.IsDefault != (i == 1) || item.Storage != []string{"s3:a", "s3:b"}[i] {
			t.Fatal(item)
		}
	}
}

func TestReferencedBucketChangesRejected(t *testing.T) {
	old := media.S3Config{ID: "a", Bucket: "photos", PublicBase: "https://img.example"}
	next := old
	next.Bucket = "other"
	for _, buckets := range [][]media.S3Config{nil, {next}} {
		if err := validateBucketChanges([]media.S3Config{old}, buckets, func(media.S3Config) (bool, error) { return true, nil }); err != errBucketReferenced {
			t.Fatal("referenced change accepted", err)
		}
		if err := validateBucketChanges([]media.S3Config{old}, buckets, func(media.S3Config) (bool, error) { return false, nil }); err != nil {
			t.Fatal("unused change rejected", err)
		}
	}
	if err := validateBucketChanges([]media.S3Config{old}, nil, func(media.S3Config) (bool, error) { return false, os.ErrPermission }); err != os.ErrPermission {
		t.Fatal("reference failure ignored", err)
	}
}

func TestReferencedBucketTargetGuard(t *testing.T) {
	old := media.S3Config{ID: "a", Bucket: "photos", Endpoint: "endpoint", Region: "region", PublicBase: "https://img.example"}
	if !bucketTargetChanged(old, nil) {
		t.Fatal("referenced deletion permitted")
	}
	for _, field := range []string{"bucket", "endpoint", "region", "url"} {
		next := old
		switch field {
		case "bucket":
			next.Bucket = "other"
		case "endpoint":
			next.Endpoint = "other"
		case "region":
			next.Region = "other"
		case "url":
			next.PublicBase = "https://other.example"
		}
		if !bucketTargetChanged(old, []media.S3Config{next}) {
			t.Fatal("retarget permitted", field)
		}
	}
	next := old
	next.Alias = "renamed"
	next.SecretKey = "rotated"
	next.Enabled = false
	next.PublicBase += "/"
	if bucketTargetChanged(old, []media.S3Config{next}) {
		t.Fatal("safe edit rejected")
	}
}

func TestImageSettingsConsumptionAndCacheIsolation(t *testing.T) {
	cfg := normalizeImageProcessing(ImageProcessingSettings{Enabled: true, Widths: []int{740, -1, 480, 740, 9000, 1080}, Quality: 101})
	if !reflect.DeepEqual(cfg.Widths, []int{480, 740, 1080}) || cfg.Quality != 82 || !cfg.Enabled {
		t.Fatal(cfg)
	}
	if !reflect.DeepEqual(configuredVariantWidths(cfg.Widths, 600), []int{480, 600}) {
		t.Fatal("source width clamp")
	}
	thumbs, err := media.NewThumbnailer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 600, 300))); err != nil {
		t.Fatal(err)
	}
	u := &MediaUploadContext{SHA256: "hash", Data: data.Bytes()}
	for _, q := range []int{82, 65} {
		cfg.Quality = q
		for _, w := range []int{480, 600} {
			if err := os.WriteFile(thumbs.PathWithQuality("upload|hash", w, q), []byte("cached"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		artifacts, err := createImageArtifacts(context.Background(), thumbs, u, cfg)
		if err != nil || len(artifacts) != 2 {
			t.Fatal(artifacts, err)
		}
		for i, w := range []int{480, 600} {
			if artifacts[i].Width != w || artifacts[i].Height != w/2 || artifacts[i].File != thumbs.PathWithQuality("upload|hash", w, q) {
				t.Fatal("settings not consumed", artifacts)
			}
		}
	}
	if thumbs.PathWithQuality("upload|hash", 480, 65) == thumbs.PathWithQuality("upload|hash", 480, 82) || thumbs.Path("upload|hash", 480) == thumbs.Path("local|hash", 480) {
		t.Fatal("cache collision")
	}
	other := thumbs.Path("local|hash", 480)
	if err := os.WriteFile(other, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	wide := thumbs.PathWithQuality("upload|hash", 8192, 65)
	if err := os.WriteFile(wide, []byte("wide"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := thumbs.Purge("upload|hash"); err != nil {
		t.Fatal(err)
	}
	if thumbs.CachedWithQuality("upload|hash", 480, 65) || thumbs.CachedWithQuality("upload|hash", 8192, 65) || !thumbs.Cached("local|hash", 480) {
		t.Fatal("purge quality/width/source isolation")
	}
}

func TestMultiBucketSensitiveSettingsAndPluginGate(t *testing.T) {
	w := httptest.NewRecorder()
	(&App{}).PutSetting(w, httptest.NewRequest("PUT", "/admin/settings?key=s3_buckets", strings.NewReader(`{"buckets":[]}`)))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if _, _, err := (&App{}).pickStorage(context.Background(), "s3:historical"); err == nil {
		t.Fatal("explicit upload bypassed plugin gate")
	}
}
