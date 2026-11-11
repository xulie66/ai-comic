package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectJSONWhitelistAndOwnership(t *testing.T) {
	ctx := context.Background()
	s, e := Open(ctx, Options{DataRoot: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.CreateUser(ctx, User{ID: "u", Username: "user"})
	p, e := s.CreateProject(ctx, "u", "book")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.LoadProjectJSON(ctx, "u", p.ID, "config/settings.json"); !errors.Is(e, ErrNotFound) {
		t.Fatal("missing file", e)
	}
	for _, rel := range []string{"config/settings.json", "projects/asset-library.json"} {
		b := json.RawMessage(`{"test":"value"}`)
		if e = s.SaveProjectJSON(ctx, "u", p.ID, rel, b); e != nil {
			t.Fatal(e)
		}
		got, e := s.LoadProjectJSON(ctx, "u", p.ID, rel)
		if e != nil || string(got) != string(b) {
			t.Fatal("roundtrip", string(got), e)
		}
		if _, e = s.LoadProjectJSON(ctx, "other", p.ID, rel); !errors.Is(e, ErrNotFound) {
			t.Fatal("cross user read", e)
		}
		if e = s.SaveProjectJSON(ctx, "other", p.ID, rel, b); !errors.Is(e, ErrNotFound) {
			t.Fatal("cross user write", e)
		}
	}
	for _, rel := range []string{"../settings.json", "config/../settings.json", "config\\settings.json", "projects/recent-project.json", "output/a.json", "/config/settings.json"} {
		if _, e = s.LoadProjectJSON(ctx, "u", p.ID, rel); !errors.Is(e, ErrForbidden) {
			t.Fatal("whitelist read", rel, e)
		}
		if e = s.SaveProjectJSON(ctx, "u", p.ID, rel, json.RawMessage(`{}`)); !errors.Is(e, ErrForbidden) {
			t.Fatal("whitelist write", rel, e)
		}
	}
	if e = s.SaveProjectJSON(ctx, "u", p.ID, "config/settings.json", json.RawMessage(`broken`)); !errors.Is(e, ErrInvalid) {
		t.Fatal("invalid JSON", e)
	}
}
func TestProjectJSONSymlinkConfinement(t *testing.T) {
	for _, rel := range []string{"config/settings.json", "projects/asset-library.json"} {
		t.Run(rel, func(t *testing.T) {
			ctx := context.Background()
			s, e := Open(ctx, Options{DataRoot: t.TempDir()})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			_ = s.CreateUser(ctx, User{ID: "u", Username: "user"})
			p, e := s.CreateProject(ctx, "u", "book")
			if e != nil {
				t.Fatal(e)
			}
			root, e := s.ProjectRoot(ctx, "u", p.ID)
			if e != nil {
				t.Fatal(e)
			}
			outside := t.TempDir()
			file := filepath.Join(outside, filepath.Base(rel))
			if e = os.WriteFile(file, []byte(`{"outside":true}`), 0600); e != nil {
				t.Fatal(e)
			}
			parent := filepath.Join(root, filepath.Dir(filepath.FromSlash(rel)))
			if e = os.Symlink(outside, parent); e != nil {
				t.Skipf("symlink privilege unavailable: %v", e)
			}
			if _, e = s.LoadProjectJSON(ctx, "u", p.ID, rel); e == nil {
				t.Fatal("symlink read accepted")
			}
			if e = s.SaveProjectJSON(ctx, "u", p.ID, rel, json.RawMessage(`{"changed":true}`)); e == nil {
				t.Fatal("symlink write accepted")
			}
			b, e := os.ReadFile(file)
			if e != nil || string(b) != `{"outside":true}` {
				t.Fatal("outside changed", e)
			}
		})
	}
}
