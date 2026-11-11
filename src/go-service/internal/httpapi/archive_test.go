package httpapi

import (
	"archive/zip"
	"bytes"
	"comic.local/go-service/internal/store"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestArchivePartsRoundTripAndCopy(t *testing.T) {
	ctx := context.Background()
	st, e := store.Open(ctx, store.Options{DataRoot: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	if e = st.CreateUser(ctx, store.User{ID: "u", Username: "user"}); e != nil {
		t.Fatal(e)
	}
	p, e := st.CreateProject(ctx, "u", "fixture")
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{Store: st}
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	jpg := []byte{255, 216, 255, 224, 0, 16, 'J', 'F', 'I', 'F', 0}
	page, e := st.WriteImage(ctx, "u", p.ID, "output/page.jpg", jpg)
	if e != nil {
		t.Fatal(e)
	}
	ref, e := st.WriteImage(ctx, "u", p.ID, "references/char.png", png)
	if e != nil {
		t.Fatal(e)
	}
	panel, e := st.WriteImage(ctx, "u", p.ID, "output/panel.png", png)
	if e != nil {
		t.Fatal(e)
	}
	state := map[string]any{"title": "fixture", "characterLibrary": []any{map[string]any{"name": "A", "referenceImagePath": ref}}, "storyboard": map[string]any{"pages": []any{map[string]any{"page_index": float64(1), "imagePath": page, "panels": []any{map[string]any{"dialogue": "你好", "imagePath": panel}}}}}}
	if e = s.saveState(ctx, "u", p.ID, state); e != nil {
		t.Fatal(e)
	}
	copied, e := s.exportAllArchive(ctx, "u", p.ID, map[string]any{})
	if e != nil {
		t.Fatal(e)
	}
	files := copied["files"].([]string)
	if len(files) != 1 || files[0] != "output/comic_page_001.jpg" {
		t.Fatal(copied)
	}
	b, e := st.ReadImage(ctx, "u", p.ID, files[0])
	if e != nil || !bytes.Equal(b, jpg) {
		t.Fatal(e)
	}
	bundle, e := s.buildArchive(ctx, "u", p.ID, "pages,script,refs,project,panels")
	if e != nil {
		t.Fatal(e)
	}
	if bundle.Counts["pages"] != 1 || bundle.Counts["refs"] != 1 || bundle.Counts["panels"] != 1 || bundle.Counts["script"] != 1 {
		t.Fatal(bundle.Counts)
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, entry := range bundle.Entries {
		f, _ := z.Create(entry.Name)
		f.Write(entry.Data)
	}
	z.Close()
	req := httptest.NewRequest("POST", "/api/project/import", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/zip")
	out, e := s.importArchive(httptest.NewRecorder(), req, "u")
	if e != nil {
		t.Fatal(e)
	}
	if out["pages"] != 1 || out["staleImages"] != 0 {
		t.Fatal(out)
	}
	np := out["project"].(store.Project)
	loaded, e := s.readState(ctx, "u", np.ID)
	if e != nil {
		t.Fatal(e)
	}
	file := pageImage(object(array(object(loaded["storyboard"])["pages"])[0]))
	if file == page {
		t.Fatal("path not relocated")
	}
	got, e := st.ReadImage(ctx, "u", np.ID, file)
	if e != nil || !bytes.Equal(got, jpg) {
		t.Fatal(e)
	}
	only, e := s.buildArchive(ctx, "u", p.ID, "script")
	if e != nil || len(only.Entries) != 1 || !strings.HasPrefix(only.Entries[0].Name, "script/") {
		t.Fatal(only, e)
	}
	raw, _ := json.Marshal(map[string]any{"name": "wrapped", "project": map[string]any{"format": "ai-comic-project", "project": state}})
	req = httptest.NewRequest("POST", "/api/project/import", bytes.NewReader(raw))
	out, e = s.importArchive(httptest.NewRecorder(), req, "u")
	if e != nil || out["pages"] != 1 || out["staleImages"] != 3 {
		t.Fatal(out, e)
	}
}
func TestArchiveRejectsTraversalDuplicatesAndLargeEntries(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "a\\b", "C:/escape", "a/../b"} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		f, _ := z.Create(name)
		f.Write([]byte("x"))
		z.Close()
		if _, _, e := readArchiveZIP(b.Bytes()); e == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for i := 0; i < 2; i++ {
		f, _ := z.Create("project.json")
		f.Write([]byte(`{"title":"x"}`))
	}
	z.Close()
	if _, _, e := readArchiveZIP(b.Bytes()); e == nil {
		t.Fatal("duplicate accepted")
	}
	b.Reset()
	z = zip.NewWriter(&b)
	f, _ := z.Create("project.json")
	f.Write([]byte(`{"title":"x"}`))
	f, _ = z.Create("large.png")
	f.Write(bytes.Repeat([]byte("a"), int(archiveFileLimit)+1))
	z.Close()
	if _, _, e := readArchiveZIP(b.Bytes()); e == nil {
		t.Fatal("oversized entry accepted")
	}
}
