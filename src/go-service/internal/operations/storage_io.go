package operations

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type StorageTarget struct {
	ID, Name, Kind, Endpoint, Bucket, Region, AccessKey, SecretKey, PublicBase, RootPrefix string
	WriteEnabled, ReadEnabled                                                              bool
	Priority                                                                               int
}

func (s *Service) target(ctx context.Context, id string) (StorageTarget, error) {
	t := StorageTarget{}
	err := s.pool.QueryRow(ctx, `SELECT id,name,kind,endpoint,bucket,region,access_key,secret_key,public_base,root_prefix,write_enabled,read_enabled,priority FROM storage_targets WHERE id=$1`, id).Scan(&t.ID, &t.Name, &t.Kind, &t.Endpoint, &t.Bucket, &t.Region, &t.AccessKey, &t.SecretKey, &t.PublicBase, &t.RootPrefix, &t.WriteEnabled, &t.ReadEnabled, &t.Priority)
	return t, err
}
func objectKey(prefix, key string) (string, error) {
	if key == "" || strings.ContainsAny(key, "\\\x00") || strings.HasPrefix(key, "/") {
		return "", errors.New("invalid object key")
	}
	combined := strings.Trim(prefix, "/") + "/" + key
	combined = strings.TrimPrefix(combined, "/")
	for _, part := range strings.Split(combined, "/") {
		if part == ".." || part == "." || strings.Contains(part, ":") {
			return "", errors.New("invalid object key segment")
		}
	}
	return combined, nil
}
func sumHex(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func mac(key []byte, v string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(v))
	return h.Sum(nil)
}
func signS3(req *http.Request, t StorageTarget, payload []byte, now time.Time) {
	date := now.UTC().Format("20060102T150405Z")
	day := date[:8]
	region := t.Region
	if region == "" {
		region = "us-east-1"
	}
	payloadHash := sumHex(payload)
	req.Header.Set("x-amz-date", date)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	headers := "host:" + req.URL.Host + "\nx-amz-content-sha256:" + payloadHash + "\nx-amz-date:" + date + "\n"
	signed := "host;x-amz-content-sha256;x-amz-date"
	canonical := strings.Join([]string{req.Method, req.URL.EscapedPath(), req.URL.Query().Encode(), headers, signed, payloadHash}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + date + "\n" + scope + "\n" + sumHex([]byte(canonical))
	key := mac(mac(mac(mac([]byte("AWS4"+t.SecretKey), day), region), "s3"), "aws4_request")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+t.AccessKey+"/"+scope+", SignedHeaders="+signed+", Signature="+hex.EncodeToString(mac(key, toSign)))
}
func (s *Service) storageIO(ctx context.Context, t StorageTarget, method, key string, data []byte, contentType string) ([]byte, error) {
	object, err := objectKey(t.RootPrefix, key)
	if err != nil {
		return nil, err
	}
	if t.Kind == "local" {
		if s.DataRoot == "" {
			return nil, errors.New("local storage root is not configured")
		}
		if err = os.MkdirAll(s.DataRoot, 0700); err != nil {
			return nil, errors.New("local storage root unavailable")
		}
		root, err := os.OpenRoot(s.DataRoot)
		if err != nil {
			return nil, errors.New("local storage root unavailable")
		}
		defer root.Close()
		name := filepath.FromSlash(object)
		switch method {
		case "PUT":
			if err = root.MkdirAll(filepath.Dir(name), 0700); err != nil {
				return nil, errors.New("local object directory failed")
			}
			f, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return nil, errors.New("local object write failed")
			}
			_, err = f.Write(data)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			return nil, err
		case "GET":
			f, err := root.Open(name)
			if err != nil {
				return nil, errors.New("local object read failed")
			}
			defer f.Close()
			return io.ReadAll(io.LimitReader(f, 32*1024*1024+1))
		case "DELETE":
			return nil, root.Remove(name)
		}
		return nil, errors.New("unsupported local operation")
	}
	if !validHTTP(t.Endpoint) {
		return nil, errors.New("invalid storage endpoint")
	}
	u, _ := url.Parse(t.Endpoint)
	isS3 := false
	switch t.Kind {
	case "s3", "oss", "cos", "kodo", "r2":
		isS3 = true
		if t.Bucket == "" || strings.ContainsAny(t.Bucket, "/\\") {
			return nil, errors.New("invalid bucket")
		}
		u.Path = path.Join(u.Path, t.Bucket, object)
	case "webdav":
		u.Path = path.Join(u.Path, object)
	default:
		return nil, errors.New("unsupported storage adapter")
	}
	u.RawQuery = ""
	u.Fragment = ""
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("storage request failed")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	if isS3 {
		signS3(req, t, data, time.Now())
	} else if t.AccessKey != "" {
		req.SetBasicAuth(t.AccessKey, t.SecretKey)
	}
	client := *s.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("storage request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("storage %s HTTP %d", method, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024+1))
}
func (s *Service) writeTarget(ctx context.Context, t StorageTarget, key string, data []byte, contentType string, probe bool) (map[string]any, error) {
	start := time.Now()
	_, err := s.storageIO(ctx, t, "PUT", key, data, contentType)
	if err == nil && probe {
		var back []byte
		back, err = s.storageIO(ctx, t, "GET", key, nil, "")
		if err == nil && !bytes.Equal(back, data) {
			err = errors.New("storage readback mismatch")
		}
		_, cleanupErr := s.storageIO(ctx, t, "DELETE", key, nil, "")
		if err == nil {
			err = cleanupErr
		}
	}
	ms := time.Since(start).Milliseconds()
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, auditErr := s.pool.Exec(auditCtx, "INSERT INTO storage_writes(target_id,object_key,bytes,ok,ms,error) VALUES($1,$2,$3,$4,$5,$6)", t.ID, key, len(data), err == nil, ms, msg)
	if auditErr != nil {
		return nil, auditErr
	}
	out := map[string]any{"id": t.ID, "name": t.Name, "kind": t.Kind, "ok": err == nil, "ms": ms, "bytes": len(data), "objectKey": key, "error": msg, "message": msg}
	if err == nil {
		out["message"] = "存储读写完成"
		if t.PublicBase != "" {
			out["url"] = strings.TrimRight(t.PublicBase, "/") + "/" + key
		}
	}
	if probe {
		_, auditErr = s.pool.Exec(auditCtx, "UPDATE storage_targets SET last_check_at=now(),last_check_ok=$2,last_check_msg=$3 WHERE id=$1", t.ID, err == nil, out["message"])
	}
	return out, auditErr
}
