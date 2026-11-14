package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Config struct {
	Endpoint       string `json:"endpoint"`
	Region         string `json:"region"`
	AccessKey      string `json:"access_key"`
	SecretKey      string `json:"secret_key"`
	SessionToken   string `json:"session_token"`
	Bucket         string `json:"bucket"`
	PublicBase     string `json:"public_base"`
	ForcePathStyle *bool  `json:"force_path_style"`
}
type Req struct {
	Version int             `json:"version"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type Resp struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}
type SaveParams struct {
	Config
	Key  string `json:"key"`
	Path string `json:"path"`
	MIME string `json:"mime"`
}
type KeyParams struct {
	Config
	Key  string `json:"key"`
	Path string `json:"path"`
}

func main() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	var req Req
	if err := dec.Decode(&req); err != nil {
		return
	}
	resp := Resp{Version: 1, ID: req.ID}
	result, err := handle(req)
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.Result = result
	}
	_ = enc.Encode(resp)
}
func client(c Config) (*s3.Client, error) {
	if c.Endpoint == "" || c.AccessKey == "" || c.SecretKey == "" {
		return nil, fmt.Errorf("endpoint/access_key/secret_key/bucket required")
	}
	region := c.Region
	if region == "" {
		region = "auto"
	}
	return s3.NewFromConfig(aws.Config{Region: region, Credentials: credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, c.SessionToken)}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(c.Endpoint)
		o.UsePathStyle = c.ForcePathStyle == nil || *c.ForcePathStyle
	}), nil
}
func handle(req Req) (any, error) {
	if req.Version != 1 {
		return nil, fmt.Errorf("unsupported protocol")
	}
	ctx := context.Background()
	switch req.Method {
	case "storage.save":
		var p SaveParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		cli, err := client(p.Config)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(p.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return nil, err
		}
		in := &s3.PutObjectInput{Bucket: aws.String(p.Bucket), Key: aws.String(p.Key), Body: f, ContentLength: aws.Int64(st.Size()), CacheControl: aws.String("public, max-age=31536000, immutable")}
		if p.MIME != "" {
			in.ContentType = aws.String(p.MIME)
		}
		_, err = cli.PutObject(ctx, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"key": p.Key, "url": strings.TrimRight(p.PublicBase, "/") + "/" + p.Key, "mime": p.MIME, "size": st.Size()}, nil
	case "storage.open":
		var p KeyParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		cli, err := client(p.Config)
		if err != nil {
			return nil, err
		}
		out, err := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(p.Bucket), Key: aws.String(p.Key)})
		if err != nil {
			return nil, err
		}
		defer out.Body.Close()
		f, err := os.OpenFile(p.Path, os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return nil, err
		}
		name := f.Name()
		_, err = io.Copy(f, out.Body)
		f.Close()
		return map[string]any{"path": name}, err
	case "storage.delete":
		var p KeyParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		cli, err := client(p.Config)
		if err != nil {
			return nil, err
		}
		_, err = cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(p.Bucket), Key: aws.String(p.Key)})
		return map[string]any{"ok": err == nil}, err
	case "storage.listBuckets":
		var p Config
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		cli, err := client(p)
		if err != nil {
			return nil, err
		}
		out, err := cli.ListBuckets(ctx, &s3.ListBucketsInput{})
		if err != nil {
			return nil, err
		}
		names := []string{}
		for _, b := range out.Buckets {
			names = append(names, aws.ToString(b.Name))
		}
		return map[string]any{"buckets": names}, nil
	case "storage.test":
		var p Config
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		cli, err := client(p)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf(".fanke-probe-%d", time.Now().UnixNano())
		_, err = cli.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(p.Bucket), Key: aws.String(key), Body: strings.NewReader("ok")})
		if err != nil {
			return nil, err
		}
		_, err = cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(p.Bucket), Key: aws.String(key)})
		return map[string]any{"ok": err == nil}, err
	case "storage.list", "storage.head", "storage.setCacheControl":
		var p struct {
			Config       Config `json:"config"`
			Key          string `json:"key"`
			Max          int    `json:"max"`
			CacheControl string `json:"cache_control"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		cli, err := client(p.Config)
		if err != nil {
			return nil, err
		}
		if req.Method == "storage.list" {
			if p.Max <= 0 {
				p.Max = 10000
			}
			if p.Max > 10000 {
				p.Max = 10000
			}
			objects := []map[string]any{}
			var token *string
			for {
				limit := p.Max - len(objects)
				if limit > 1000 {
					limit = 1000
				}
				out, err := cli.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(p.Config.Bucket), ContinuationToken: token, MaxKeys: aws.Int32(int32(limit))})
				if err != nil {
					return nil, err
				}
				for _, o := range out.Contents {
					objects = append(objects, map[string]any{"key": aws.ToString(o.Key), "size": aws.ToInt64(o.Size), "etag": aws.ToString(o.ETag), "last_modified": o.LastModified})
				}
				more := aws.ToBool(out.IsTruncated)
				if !more || len(objects) >= p.Max {
					return map[string]any{"objects": objects, "truncated": more}, nil
				}
				if out.NextContinuationToken == nil || aws.ToString(out.NextContinuationToken) == aws.ToString(token) {
					return nil, fmt.Errorf("invalid continuation token")
				}
				token = out.NextContinuationToken
			}
		}
		head, err := cli.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(p.Config.Bucket), Key: aws.String(p.Key)})
		if err != nil {
			return nil, err
		}
		if req.Method == "storage.head" {
			return map[string]any{"size": aws.ToInt64(head.ContentLength), "mime": aws.ToString(head.ContentType), "cache_control": aws.ToString(head.CacheControl)}, nil
		}
		_, err = cli.CopyObject(ctx, &s3.CopyObjectInput{Bucket: aws.String(p.Config.Bucket), Key: aws.String(p.Key), CopySource: aws.String(url.PathEscape(p.Config.Bucket + "/" + p.Key)), CopySourceIfMatch: head.ETag, MetadataDirective: types.MetadataDirectiveReplace, Metadata: head.Metadata, CacheControl: aws.String(p.CacheControl), ContentType: head.ContentType, ContentDisposition: head.ContentDisposition, ContentEncoding: head.ContentEncoding, ContentLanguage: head.ContentLanguage, Expires: head.Expires})
		return map[string]any{"ok": err == nil}, err
	default:
		return nil, fmt.Errorf("unsupported method")
	}
}

var _ = types.ObjectIdentifier{}
