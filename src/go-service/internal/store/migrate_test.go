package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyMigrationCopyAndRollback(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, e := Open(ctx, Options{DataRoot: root})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.CreateUser(ctx, User{ID: "u", Username: "user"})
	e = s.mutate(func(d *snapshot) error {
		d.Projects["default"] = Project{ID: "default", OwnerID: "u", Root: ".", Legacy: true}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	for rel, body := range map[string]string{"projects/recent-project.json": `{"storyboard":{"pages":[{"page_index":1,"page_image_url":"/data/output/a.png"}]}}`, "output/a.png": "image", "references/r.png": "reference", "config/settings.json": `{"private":"config"}`, "assets/a.txt": "asset", "projects/asset-library.json": `{"characters":[{"name":"Alice","referenceImagePath":"/data/references/r.png"}]}`, "logs/task.log": "old-log"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(p, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	plans, e := s.LegacyMigrationPlans(ctx)
	if e != nil || len(plans) != 1 || len(plans[0].Paths) != 7 {
		t.Fatalf("plan %+v %v", plans, e)
	}
	if _, e = os.Stat(filepath.Join(root, "projects", "default")); !os.IsNotExist(e) {
		t.Fatal("dry run changed files")
	}
	result, e := s.ApplyLegacyMigration(ctx, "default")
	if e != nil {
		t.Fatal(e)
	}
	if result.Status != "committed" {
		t.Fatal(result)
	}
	p, e := s.GetProject(ctx, "u", "default")
	if e != nil || p.Root != "projects/default" || p.Legacy {
		t.Fatal(p, e)
	}
	b, e := s.ReadImage(ctx, "u", "default", "output/a.png")
	if e != nil || string(b) != "image" {
		t.Fatal(e)
	}
	b, e = s.LoadState(ctx, "u", "default")
	if e != nil || !strings.Contains(string(b), "projects/default/output/a.png") {
		t.Fatalf("state rewrite: %s %v", b, e)
	}
	original, e := os.ReadFile(filepath.Join(root, "projects", "recent-project.json"))
	if e != nil || !strings.Contains(string(original), "/data/output/a.png") {
		t.Fatal("original was modified")
	}
	asset, e := os.ReadFile(filepath.Join(root, "projects", "default", "projects", "asset-library.json"))
	if e != nil || !strings.Contains(string(asset), "projects/default/references/r.png") {
		t.Fatalf("asset library rewrite: %s %v", asset, e)
	}
	log, e := os.ReadFile(filepath.Join(root, "projects", "default", "logs", "task.log"))
	if e != nil || string(log) != "old-log" {
		t.Fatal("log migration", e)
	}
	manifest := filepath.Join(result.Backup, "manifest.json")
	b, e = os.ReadFile(manifest)
	if e != nil {
		t.Fatal(e)
	}
	var saved MigrationPlan
	if e = json.Unmarshal(b, &saved); e != nil || saved.Status != "committed" {
		t.Fatal(string(b), e)
	}
	if e = s.RollbackLegacyMigration(ctx, manifest); e != nil {
		t.Fatal(e)
	}
	p, _ = s.GetProject(ctx, "u", "default")
	if p.Root != "." || !p.Legacy {
		t.Fatal("rollback failed", p)
	}
	if _, e = os.Stat(filepath.Join(root, "projects", "default", "output", "a.png")); e != nil {
		t.Fatal("rollback removed copy", e)
	}
}
func TestLegacyMigrationExistingTargetStops(t *testing.T) {
	ctx := context.Background()
	s, e := Open(ctx, Options{DataRoot: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.mutate(func(d *snapshot) error {
		d.Projects["default"] = Project{ID: "default", OwnerID: "u", Root: "."}
		return nil
	})
	_ = os.MkdirAll(filepath.Join(s.root, "projects", "default"), 0700)
	_ = os.MkdirAll(filepath.Join(s.root, "output"), 0700)
	if _, e = s.ApplyLegacyMigration(ctx, "default"); e == nil {
		t.Fatal("existing target accepted")
	}
	p, _ := s.GetProject(ctx, "u", "default")
	if p.Root != "." {
		t.Fatal("metadata changed")
	}
}
