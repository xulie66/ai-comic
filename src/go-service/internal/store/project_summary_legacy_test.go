package store

import (
	"context"
	"errors"
	"testing"
)

func TestSummaryLegacyDoesNotBlockProjectList(t *testing.T) {
	ctx := context.Background()
	s, e := Open(ctx, Options{DataRoot: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.CreateUser(ctx, User{ID: "u", Username: "user"})
	_ = s.CreateUser(ctx, User{ID: "other", Username: "other"})
	if e = s.mutate(func(d *snapshot) error {
		d.Projects["default"] = Project{ID: "default", OwnerID: "u", Name: "Legacy comic", Root: ".", Legacy: true}
		d.Active["u"] = "default"
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	normal, e := s.CreateProject(ctx, "u", "New comic")
	if e != nil {
		t.Fatal(e)
	}
	summary, e := s.ProjectSummary(ctx, "u", "default")
	if e != nil || summary["needsMigration"] != true || summary["pageCount"] != 0 || summary["cover"] != "" || summary["active"] != true {
		t.Fatal("legacy fallback", summary, e)
	}
	list, e := s.ListProjects(ctx, "u")
	if e != nil {
		t.Fatal(e)
	}
	cards, e := s.SummarizeProjects(ctx, "u", list)
	if e != nil || len(cards) != 2 {
		t.Fatal("legacy blocked list", cards, e)
	}
	summary, e = s.ProjectSummary(ctx, "u", normal.ID)
	if e != nil || summary["needsMigration"] != false {
		t.Fatal("normal project marked migration", summary, e)
	}
	if _, e = s.ProjectSummary(ctx, "other", "default"); !errors.Is(e, ErrNotFound) {
		t.Fatal("owner isolation bypass", e)
	}
	if _, e = s.LoadState(ctx, "u", "default"); !errors.Is(e, ErrForbidden) {
		t.Fatal("legacy shared root opened", e)
	}
}
