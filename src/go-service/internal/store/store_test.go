package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestTenantPersistenceAndFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := Open(ctx, Options{DataRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"u1", "u2"} {
		if err = s.CreateUser(ctx, User{ID: id, Username: id}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.CreateProject(ctx, "u1", "Book")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetProject(ctx, "u2", p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("project isolation", err)
	}
	if err = s.SaveState(ctx, "u2", p.ID, json.RawMessage(`{"bad":true}`)); !errors.Is(err, ErrNotFound) {
		t.Fatal("state isolation", err)
	}
	state := json.RawMessage(`{"originalText":"hello","storyboard":{"pages":[]}}`)
	if err = s.SaveState(ctx, "u1", p.ID, state); err != nil {
		t.Fatal(err)
	}
	if err = s.SetActiveProject(ctx, "u1", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteImage(ctx, "u1", p.ID, "output/panels/a.png", []byte("image")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"../a.png", "output/../../a.png", "/output/a.png", "output\\a.png", "output/a.png:stream", "config/settings.png", "output/a.txt"} {
		if _, err = s.WriteImage(ctx, "u1", p.ID, rel, []byte("bad")); err == nil {
			t.Fatal("accepted invalid path", rel)
		}
	}
	if _, err = s.ReadImage(ctx, "u2", p.ID, "output/panels/a.png"); !errors.Is(err, ErrNotFound) {
		t.Fatal("image isolation", err)
	}
	s.Close()
	s, err = Open(ctx, Options{DataRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.LoadState(ctx, "u1", p.ID)
	if err != nil || string(got) != string(state) {
		t.Fatalf("state persistence %s %v", got, err)
	}
	if _, err = s.ActiveProject(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	got, err = s.ReadImage(ctx, "u1", p.ID, "output/panels/a.png")
	if err != nil || string(got) != "image" {
		t.Fatal(err)
	}
	if err = s.DeleteProject(ctx, "u2", p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err = s.DeleteProject(ctx, "u1", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ActiveProject(ctx, "u1"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.LoadState(ctx, "u1", p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted project accessible", err)
	}
}
func TestConcurrentRegistrationUnique(t *testing.T) {
	s, err := Open(context.Background(), Options{DataRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	ch := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch <- s.CreateUser(context.Background(), User{ID: NewID("u"), Username: "same"})
		}()
	}
	wg.Wait()
	close(ch)
	success := 0
	for err := range ch {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("duplicate users", success)
	}
}
func TestSymlinkIsolation(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DataRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CreateUser(ctx, User{ID: "u", Username: "u"}); err != nil {
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
	outside := t.TempDir()
	if err = os.Symlink(outside, filepath.Join(root, "output")); err != nil {
		t.Skipf("symlink privilege unavailable: %v", err)
	}
	if _, err = s.WriteImage(ctx, "u", p.ID, "output/a.png", []byte("bad")); err == nil {
		t.Fatal("symlink accepted")
	}
}
