package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestProjectSummaryStatisticsAndOwnership(t *testing.T) {
	ctx := context.Background()
	s, e := Open(ctx, Options{DataRoot: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.CreateUser(ctx, User{ID: "alice", Username: "alice"})
	_ = s.CreateUser(ctx, User{ID: "bob", Username: "bob"})
	p, e := s.CreateProject(ctx, "alice", "Book")
	if e != nil {
		t.Fatal(e)
	}
	state := json.RawMessage(`{"originalText":"  A story\n about a hero  ","characterLibrary":[{"name":"A"},{"name":"B"}],"storyboard":{"pages":[{"page_index":1,"page_image_url":"/data/projects/` + p.ID + `/output/panels/a b.png","panels":[{},{}]},{"page_index":2,"imagePath":"references/b.png","panels":[{}]},{"page_index":3,"pageImageUrl":"output/c.png","panels":[]},{"page_index":4,"panels":[{},{}]}]}}`)
	if e = s.SaveState(ctx, "alice", p.ID, state); e != nil {
		t.Fatal(e)
	}
	_ = s.SetActiveProject(ctx, "alice", p.ID)
	summary, e := s.ProjectSummary(ctx, "alice", p.ID)
	if e != nil {
		t.Fatal(e)
	}
	for k, want := range map[string]int{"pageCount": 4, "doneCount": 3, "panelCount": 5, "characterCount": 2} {
		if summary[k] != want {
			t.Fatalf("%s=%v want %d", k, summary[k], want)
		}
	}
	if summary["title"] != "A story about a hero" || summary["active"] != true {
		t.Fatal(summary)
	}
	cover := summary["cover"].(string)
	parsed, e := url.Parse(cover)
	if e != nil || parsed.Path != "/files" || parsed.Query().Get("projectId") != p.ID || parsed.Query().Get("p") != "output/panels/a b.png" {
		t.Fatal("cover", cover, e)
	}
	raw, _ := json.Marshal(summary)
	if strings.Contains(string(raw), s.root) || strings.Contains(string(raw), "/data/") || summary["root"] != nil || summary["absPath"] != nil {
		t.Fatal("server path leaked", string(raw))
	}
	if _, e = s.ProjectSummary(ctx, "bob", p.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross owner", e)
	}
	if _, e = s.SummarizeProjects(ctx, "bob", []Project{p}); !errors.Is(e, ErrNotFound) {
		t.Fatal("forged metadata bypass", e)
	}
	list, e := s.SummarizeProjects(ctx, "alice", []Project{p})
	if e != nil || len(list) != 1 {
		t.Fatal(list, e)
	}
}
func TestProjectSummaryEmptyAndForeignCover(t *testing.T) {
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
	summary, e := s.ProjectSummary(ctx, "u", p.ID)
	if e != nil || summary["pageCount"] != 0 || summary["active"] != false {
		t.Fatal(summary, e)
	}
	if e = s.SaveState(ctx, "u", p.ID, json.RawMessage(`{"storyboard":{"characterLibrary":[{}],"pages":[{"page_index":1,"imagePath":"/data/projects/other/output/a.png"}]}}`)); e != nil {
		t.Fatal(e)
	}
	summary, e = s.ProjectSummary(ctx, "u", p.ID)
	if e != nil || summary["cover"] != "" || summary["characterCount"] != 1 {
		t.Fatal("foreign cover exposed", summary, e)
	}
}
