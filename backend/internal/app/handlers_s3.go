package app

import (
	"context"
	"errors"
	"fanke/backend/internal/media"
	"fanke/backend/internal/repo"
	"net/http"
	"strings"
)

const secretMask = "********"

type s3SettingsResponse struct {
	Buckets         []media.S3Config `json:"buckets"`
	DefaultBucketID string           `json:"default_bucket_id"`
}

func maskSecret(c media.S3Config) media.S3Config {
	if c.SecretKey != "" {
		c.SecretKey = secretMask
	}
	if c.SessionToken != "" {
		c.SessionToken = secretMask
	}
	return c
}
func maskSettings(s repo.S3Settings) s3SettingsResponse {
	out := s3SettingsResponse{DefaultBucketID: s.DefaultBucketID}
	for _, c := range s.Buckets {
		out.Buckets = append(out.Buckets, maskSecret(c))
	}
	return out
}

func (a *App) GetS3Config(w http.ResponseWriter, r *http.Request) {
	s, err := a.DB.S3Settings(r.Context())
	if err != nil {
		a.failInternal(w, r, "s3 config", err)
		return
	}
	writeJSON(w, http.StatusOK, maskSettings(s))
}
func (a *App) PutS3Config(w http.ResponseWriter, r *http.Request) {
	var in repo.S3Settings
	if err := decodeJSON(r, &in); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	old, err := a.DB.S3Settings(r.Context())
	if err != nil {
		a.failInternal(w, r, "read s3 config", err)
		return
	}
	oldByID := map[string]media.S3Config{}
	for _, c := range old.Buckets {
		oldByID[c.ID] = c
	}
	ids := map[string]bool{}
	for i := range in.Buckets {
		c := &in.Buckets[i]
		if c.ID == "" {
			c.ID = stableIDForAPI(*c)
		}
		if ids[c.ID] {
			a.fail(w, http.StatusBadRequest, "duplicate bucket id")
			return
		}
		ids[c.ID] = true
		if prev, ok := oldByID[c.ID]; ok {
			*c = preserveBucketCredentials(*c, prev)
		}
		if c.Enabled && (c.Endpoint == "" || c.AccessKey == "" || c.SecretKey == "" || c.Bucket == "") {
			a.fail(w, http.StatusBadRequest, "启用时 endpoint/access_key/bucket 必填")
			return
		}
	}
	if in.DefaultBucketID != "" && !ids[in.DefaultBucketID] {
		a.fail(w, http.StatusBadRequest, "default_bucket_id 不存在")
		return
	}
	if err := validateBucketChanges(old.Buckets, in.Buckets, func(c media.S3Config) (bool, error) { return a.bucketReferenced(r.Context(), c) }); err != nil {
		if errors.Is(err, errBucketReferenced) {
			a.fail(w, http.StatusConflict, err.Error())
		} else {
			a.failInternal(w, r, "check bucket references", err)
		}
		return
	}
	if err := a.DB.PutS3Settings(r.Context(), in); err != nil {
		a.failInternal(w, r, "save s3 config", err)
		return
	}
	a.s3Mu.Lock()
	a.s3Hash = ""
	a.s3Mu.Unlock()
	writeJSON(w, http.StatusOK, maskSettings(in))
}

var errBucketReferenced = errors.New("桶仍被媒体引用，禁止删除或改变指向")

func validateBucketChanges(old, next []media.S3Config, referenced func(media.S3Config) (bool, error)) error {
	for _, c := range old {
		if !bucketTargetChanged(c, next) {
			continue
		}
		used, err := referenced(c)
		if err != nil {
			return err
		}
		if used {
			return errBucketReferenced
		}
	}
	return nil
}

func bucketTargetChanged(old media.S3Config, buckets []media.S3Config) bool {
	next, ok := findBucket(buckets, old.ID)
	return !ok || next.Bucket != old.Bucket || strings.TrimRight(next.PublicBase, "/") != strings.TrimRight(old.PublicBase, "/") || next.Endpoint != old.Endpoint || next.Region != old.Region
}

func preserveBucketCredentials(c, prev media.S3Config) media.S3Config {
	if c.SecretKey == "" || c.SecretKey == secretMask {
		c.SecretKey = prev.SecretKey
	}
	if c.SessionToken == "" || c.SessionToken == secretMask {
		c.SessionToken = prev.SessionToken
	}
	if c.AccessKey == "" || c.AccessKey == secretMask {
		c.AccessKey = prev.AccessKey
	}
	if c.ForcePathStyle == nil {
		c.ForcePathStyle = prev.ForcePathStyle
	}
	return c
}

func stableIDForAPI(c media.S3Config) string { return repo.StableBucketID(c) }
func findBucket(bs []media.S3Config, id string) (media.S3Config, bool) {
	for _, b := range bs {
		if b.ID == id {
			return b, true
		}
	}
	return media.S3Config{}, false
}
func (a *App) bucketReferenced(ctx context.Context, c media.S3Config) (bool, error) {
	recs, err := a.DB.AllMedia(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range recs {
		if recordInBucket(m.Storage, m.URL, m.Key, c) {
			return true, nil
		}
	}
	variants, err := a.DB.MediaVariants(ctx, 0)
	if err != nil {
		return false, err
	}
	for _, v := range variants {
		if recordInBucket(v.Storage, v.URL, v.Key, c) {
			return true, nil
		}
	}
	return false, nil
}

func (a *App) s3CredsFrom(r *http.Request, in media.S3Config) media.S3Config {
	s, _ := a.DB.S3Settings(r.Context())
	for _, c := range s.Buckets {
		if (in.ID != "" && in.ID == c.ID) || (in.ID == "" && in.Bucket == c.Bucket && in.Endpoint == c.Endpoint) {
			if in.SecretKey == "" || in.SecretKey == secretMask {
				in.SecretKey = c.SecretKey
			}
			if in.SessionToken == "" || in.SessionToken == secretMask {
				in.SessionToken = c.SessionToken
			}
			return in
		}
	}
	return in
}
func (a *App) S3Buckets(w http.ResponseWriter, r *http.Request) {
	var in media.S3Config
	_ = decodeJSON(r, &in)
	in = a.s3CredsFrom(r, in)
	names, err := media.ListBuckets(r.Context(), in)
	if err != nil {
		a.fail(w, http.StatusBadGateway, "获取桶列表失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"buckets": names})
}
func (a *App) S3Test(w http.ResponseWriter, r *http.Request) {
	var in media.S3Config
	_ = decodeJSON(r, &in)
	in = a.s3CredsFrom(r, in)
	if in.Bucket == "" {
		a.fail(w, http.StatusBadRequest, "bucket required")
		return
	}
	if err := media.Test(r.Context(), in); err != nil {
		a.fail(w, http.StatusBadGateway, "连通性测试失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
