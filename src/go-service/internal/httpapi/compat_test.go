package httpapi

import (
	"comic.local/go-service/internal/store"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompatTasksCopyScenesAndStats(t *testing.T) {
	ctx := context.Background()
	st, e := store.Open(ctx, store.Options{DataRoot: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	for _, id := range []string{"u", "other"} {
		if e = st.CreateUser(ctx, store.User{ID: id, Username: id}); e != nil {
			t.Fatal(e)
		}
	}
	p, e := st.CreateProject(ctx, "u", "project")
	if e != nil {
		t.Fatal(e)
	}
	other, e := st.CreateProject(ctx, "other", "private")
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{Store: st}
	task, e := s.compatTask(ctx, "u", p.ID, "/api/task/create", map[string]any{"apiKey": "must-not-persist"})
	if e != nil {
		t.Fatal(e)
	}
	id := str(task, "taskId")
	root, _ := st.ProjectRoot(ctx, "u", p.ID)
	raw, e := os.ReadFile(filepath.Join(root, "logs/tasks", id+".json"))
	if e != nil || strings.Contains(string(raw), "must-not-persist") {
		t.Fatalf("private state %s %v", raw, e)
	}
	done, e := s.compatTask(ctx, "u", p.ID, "/api/task/complete", map[string]any{"taskId": id, "data": map[string]any{"totalPages": float64(2), "key": "private"}})
	if e != nil || done["state"] != "completed" {
		t.Fatal(done, e)
	}
	if _, e = s.compatTask(ctx, "other", other.ID, "/api/task/complete", map[string]any{"taskId": id}); e == nil {
		t.Fatal("foreign task completed")
	}
	if _, e = s.compatTask(ctx, "u", p.ID, "/api/task/complete", map[string]any{"taskId": "task_../../escape"}); e == nil {
		t.Fatal("invalid id accepted")
	}
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	source, e := st.WriteImage(ctx, "u", p.ID, "output/source.png", png)
	if e != nil {
		t.Fatal(e)
	}
	copied, e := s.compatCopy(ctx, "u", p.ID, map[string]any{"sourcePath": source})
	if e != nil || str(copied, "path") == source {
		t.Fatal(copied, e)
	}
	b, e := st.ReadImage(ctx, "u", p.ID, str(copied, "path"))
	if e != nil || string(b) != string(png) {
		t.Fatal(e)
	}
	if _, e = s.compatCopy(ctx, "u", p.ID, map[string]any{"sourcePath": source, "sourceProjectId": other.ID}); e == nil {
		t.Fatal("foreign source accepted")
	}
	state := map[string]any{"characterLibrary": []any{map[string]any{"name": "A"}}, "sceneLibrary": []any{map[string]any{"name": "卧室", "aliases": []string{"房间"}, "refImagePath": source}, map[string]any{"name": "无关地点"}}, "storyboard": map[string]any{"pages": []any{map[string]any{"page_index": float64(1), "scene": "房间", "imagePath": source, "panels": []any{map[string]any{"scene": "房间"}}}}}}
	if e = s.saveState(ctx, "u", p.ID, state); e != nil {
		t.Fatal(e)
	}
	out, e := s.compatScenes(ctx, "u", p.ID, map[string]any{"dryRun": true})
	if e != nil || len(array(out["library"])) != 1 {
		t.Fatal(out, e)
	}
	current, _ := s.readState(ctx, "u", p.ID)
	if str(object(array(object(current["storyboard"])["pages"])[0]), "scene") != "房间" {
		t.Fatal("dryRun wrote state")
	}
	out, e = s.compatScenes(ctx, "u", p.ID, map[string]any{})
	if e != nil {
		t.Fatal(e)
	}
	if object(out["canonical"])["renamed"] != 2 {
		t.Fatal(out)
	}
	current, _ = s.readState(ctx, "u", p.ID)
	if str(object(array(object(current["storyboard"])["pages"])[0]), "scene") != "卧室" {
		t.Fatal(current)
	}
	rows, e := s.compatProjects(ctx, "u")
	if e != nil || len(rows) != 1 || rows[0]["pageCount"] != 1 || rows[0]["doneCount"] != 1 || rows[0]["characterCount"] != 1 {
		t.Fatal(rows, e)
	}
}
