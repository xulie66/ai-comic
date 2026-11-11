package httpapi

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootEntryWithoutRedirect(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ui"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ui", "index.html"), []byte(`<html><script src="/ui/assets/app.js"></script>创作台</html>`), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{PublicDir: dir}
	for _, path := range []string{"/", "/?token=fixture", "/ui/"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		s.static(w, r)
		if w.Code != 200 || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), "创作台") {
			t.Fatalf("%s: status=%d location=%s body=%s", path, w.Code, w.Header().Get("Location"), w.Body.String())
		}
	}
}
