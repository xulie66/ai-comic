package auth

import (
	"comic.local/go-service/internal/store"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacyJSONImportLoginAndImage(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	salt, hash, e := HashPassword("old-password")
	if e != nil {
		t.Fatal(e)
	}
	write := func(rel string, v any) {
		t.Helper()
		file := filepath.Join(root, filepath.FromSlash(rel))
		if e = os.MkdirAll(filepath.Dir(file), 0700); e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(v)
		if e = os.WriteFile(file, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("accounts.json", map[string]any{"users": []any{map[string]any{"id": "usr_old", "username": "Alice", "displayName": "Alice", "salt": salt, "hash": hash, "createdAt": time.Now().UTC().Format(time.RFC3339)}}})
	write("sessions.json", map[string]any{"sessions": map[string]any{"legacy-token": map[string]any{"userId": "usr_old", "expiresAt": time.Now().Add(time.Hour).UnixMilli()}}})
	write("projects/index.json", map[string]any{"items": []any{map[string]any{"id": "default", "root": ".", "name": "Old comic", "legacy": true}}, "activeId": "default"})
	write("projects/recent-project.json", map[string]any{"storyboard": map[string]any{"pages": []any{map[string]any{"page_index": 1, "page_image_url": "/data/output/one.png"}}}})
	if e = os.MkdirAll(filepath.Join(root, "output"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "output", "one.png"), []byte("image"), 0600); e != nil {
		t.Fatal(e)
	}
	plan, e := store.ImportLegacyJSON(ctx, root, false)
	if e != nil || plan.Users != 1 || plan.OwnerAssignments["default"] != "usr_old" {
		t.Fatalf("plan %+v %v", plan, e)
	}
	if _, e = os.Stat(filepath.Join(root, "go-store.json")); !os.IsNotExist(e) {
		t.Fatal("dry run wrote snapshot")
	}
	plan, e = store.ImportLegacyJSON(ctx, root, true)
	if e != nil {
		t.Fatal(e)
	}
	s, e := store.Open(ctx, store.Options{DataRoot: root})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a := New(s, Options{})
	logged, e := a.Login(ctx, Credentials{Username: "alice", Password: "old-password"})
	if e != nil || logged.User.ID != "usr_old" {
		t.Fatalf("legacy login %+v %v", logged, e)
	}
	if _, e = a.Session(ctx, "legacy-token"); e != nil {
		t.Fatal("legacy session", e)
	}
	p, e := s.ActiveProject(ctx, "usr_old")
	if e != nil || p.ID != "default" {
		t.Fatal("active project", p, e)
	}
	b, e := s.LoadState(ctx, "usr_old", p.ID)
	if e != nil {
		t.Fatal(e)
	}
	var state map[string]any
	_ = json.Unmarshal(b, &state)
	path := state["storyboard"].(map[string]any)["pages"].([]any)[0].(map[string]any)["page_image_url"].(string)
	b, e = s.ReadImage(ctx, "usr_old", p.ID, path)
	if e != nil || string(b) != "image" {
		t.Fatal("imported image", path, e)
	}
	if _, e = store.ImportLegacyJSON(ctx, root, true); e == nil {
		t.Fatal("overwrote existing snapshot")
	}
	if _, e = os.Stat(filepath.Join(plan.Backup, "accounts.json")); e != nil {
		t.Fatal("backup missing", e)
	}
}
