package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestLegacyAbsoluteImageAndStateRoundtrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DataRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CreateUser(ctx, User{ID: "u", Username: "alice"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProject(ctx, "u", "book")
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.ProjectRoot(ctx, "u", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteImage(ctx, "u", p.ID, "output/panels/one.png", []byte("image")); err != nil {
		t.Fatal(err)
	}
	legacy := "/data/projects/" + p.ID + "/output/panels/one.png"
	for _, path := range []string{legacy, filepath.Join(root, "output", "panels", "one.png")} {
		b, err := s.ReadImage(ctx, "u", p.ID, path)
		if err != nil || string(b) != "image" {
			t.Fatalf("compat path %s: %v", path, err)
		}
	}
	for _, path := range []string{"/data/projects/other/output/panels/one.png", "/data/projects/" + p.ID + "/../other/output/a.png", root + "-other/output/a.png"} {
		if _, err = s.ReadImage(ctx, "u", p.ID, path); err == nil {
			t.Fatal("accepted foreign absolute path", path)
		}
	}
	state, _ := json.Marshal(map[string]any{"storyboard": map[string]any{"pages": []map[string]any{{"page_image_url": legacy, "imageHistory": []string{legacy}}}}})
	if err = s.SaveState(ctx, "u", p.ID, state); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		loaded, err := s.LoadState(ctx, "u", p.ID)
		if err != nil || string(loaded) != string(state) {
			t.Fatalf("image fields lost: %s %v", loaded, err)
		}
		if err = s.SaveState(ctx, "u", p.ID, loaded); err != nil {
			t.Fatal(err)
		}
	}
}
