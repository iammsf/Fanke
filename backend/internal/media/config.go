package media

import (
	"context"
	"fmt"
	"fanke/backend/internal/pluginrpc"
)

type S3Config struct {
	ID             string `json:"id,omitempty"`
	Alias          string `json:"alias,omitempty"`
	DisplayName    string `json:"display_name,omitempty"`
	Prefix         string `json:"prefix,omitempty"`
	ForcePathStyle *bool  `json:"force_path_style"`
	SessionToken   string `json:"session_token"`
	Enabled        bool   `json:"enabled"`
	Endpoint       string `json:"endpoint"`
	Region         string `json:"region"`
	AccessKey      string `json:"access_key"`
	SecretKey      string `json:"secret_key"`
	Bucket         string `json:"bucket"`
	PublicBase     string `json:"public_base"`
	Allowed        string `json:"allowed"`
	FolderByType   bool   `json:"folder_by_type"`
}

func NewS3FromConfig(ctx context.Context, c S3Config) (*S3, error) {
	if c.Bucket == "" {
		return nil, fmt.Errorf("bucket required")
	}
	return &S3{cfg: c, runner: newS3Runner()}, nil
}
func ListBuckets(ctx context.Context, c S3Config) ([]string, error) {
	var out struct {
		Buckets []string `json:"buckets"`
	}
	err := newS3Runner().Call(ctx, "storage.listBuckets", c, &out)
	return out.Buckets, err
}
func Test(ctx context.Context, c S3Config) error {
	var out any
	return newS3Runner().Call(ctx, "storage.test", c, &out)
}

var _ = pluginrpc.ProtocolVersion
