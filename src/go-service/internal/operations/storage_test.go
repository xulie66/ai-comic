package operations

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLocalStorageConfinement(t *testing.T) {
	root := t.TempDir()
	s := New(nil, nil)
	s.DataRoot = root
	target := StorageTarget{Kind: "local", RootPrefix: "objects"}
	ctx := context.Background()
	if _, err := s.storageIO(ctx, target, "PUT", "a/b.txt", []byte("hello"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	back, err := s.storageIO(ctx, target, "GET", "a/b.txt", nil, "")
	if err != nil || string(back) != "hello" {
		t.Fatalf("%s %v", back, err)
	}
	for _, key := range []string{"../escape", "a/../../escape", "/absolute", "C:/escape", "a\\escape"} {
		if _, err = s.storageIO(ctx, target, "PUT", key, []byte("x"), ""); err == nil {
			t.Fatal("path accepted", key)
		}
	}
	outside := t.TempDir()
	if err = os.Symlink(outside, filepath.Join(root, "objects", "escape")); err == nil {
		if _, err = s.storageIO(ctx, target, "PUT", "escape/x", []byte("x"), ""); err == nil {
			t.Fatal("symlink escape accepted")
		}
	}
	if _, err = s.storageIO(ctx, target, "DELETE", "a/b.txt", nil, ""); err != nil {
		t.Fatal(err)
	}
}
func TestRemoteStorageAdapters(t *testing.T) {
	for _, kind := range []string{"s3", "webdav"} {
		t.Run(kind, func(t *testing.T) {
			objects := map[string][]byte{}
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "s3" {
					if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=ACCESS/") || r.Header.Get("x-amz-content-sha256") == "" {
						t.Error("missing SigV4")
					}
				} else {
					u, p, ok := r.BasicAuth()
					if !ok || u != "ACCESS" || p != "SECRET" {
						t.Error("missing Basic auth")
					}
				}
				mu.Lock()
				defer mu.Unlock()
				switch r.Method {
				case "PUT":
					raw, _ := io.ReadAll(r.Body)
					objects[r.URL.Path] = raw
					w.WriteHeader(201)
				case "GET":
					w.Write(objects[r.URL.Path])
				case "DELETE":
					delete(objects, r.URL.Path)
					w.WriteHeader(204)
				}
			}))
			defer server.Close()
			s := New(nil, nil)
			target := StorageTarget{Kind: kind, Endpoint: server.URL, Bucket: "bucket", RootPrefix: "prefix", AccessKey: "ACCESS", SecretKey: "SECRET"}
			ctx := context.Background()
			if _, err := s.storageIO(ctx, target, "PUT", "a space/中文.txt", []byte("payload"), "text/plain"); err != nil {
				t.Fatal(err)
			}
			raw, err := s.storageIO(ctx, target, "GET", "a space/中文.txt", nil, "")
			if err != nil || string(raw) != "payload" {
				t.Fatalf("%s %v", raw, err)
			}
			if _, err = s.storageIO(ctx, target, "DELETE", "a space/中文.txt", nil, ""); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestS3SignatureDeterministic(t *testing.T) {
	target := StorageTarget{AccessKey: "AKID", SecretKey: "SECRET", Region: "us-east-1"}
	req, _ := http.NewRequest("PUT", "https://example.test/bucket/a%20b.txt", strings.NewReader("test"))
	date := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	signS3(req, target, []byte("test"), date)
	auth := req.Header.Get("Authorization")
	if !strings.Contains(auth, "20240102/us-east-1/s3/aws4_request") || req.Header.Get("x-amz-date") != "20240102T030405Z" {
		t.Fatal(auth)
	}
	req2, _ := http.NewRequest("PUT", req.URL.String(), nil)
	signS3(req2, target, []byte("test"), date)
	if auth != req2.Header.Get("Authorization") {
		t.Fatal("unstable signature")
	}
}
