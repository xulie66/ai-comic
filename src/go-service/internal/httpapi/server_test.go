package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"comic.local/go-service/internal/auth"
	"comic.local/go-service/internal/generation"
	"comic.local/go-service/internal/operations"
	"comic.local/go-service/internal/store"
)

// All upstream traffic in this file goes to an in-process httptest provider.
type regressionAPI struct {
	t      *testing.T
	server *Server
	cookie *http.Cookie
}

func newRegressionAPI(t *testing.T, admin bool) *regressionAPI {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{DataRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := auth.New(st, auth.Options{CookieName: "test_session"})
	password := "local-fixture-password"
	if admin {
		_, err = a.BootstrapAdmin(context.Background(), "fixture_admin", password)
	} else {
		_, err = a.Register(context.Background(), auth.Credentials{Username: "fixture_user", Password: password})
	}
	if err != nil {
		t.Fatal(err)
	}
	api := &regressionAPI{t: t, server: &Server{Store: st, Auth: a, Ops: operations.New(nil, logger), Client: &generation.Client{Logger: logger}, Logger: logger}}
	name := "fixture_user"
	if admin {
		name = "fixture_admin"
	}
	login := api.request(http.MethodPost, "/api/auth/login", map[string]any{"username": name, "password": password}, false)
	api.status(login, 200)
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "test_session" || cookies[0].Value == "" || !cookies[0].HttpOnly || cookies[0].Path != "/" {
		t.Fatalf("invalid login cookie: %v", cookies)
	}
	api.cookie = cookies[0]
	api.status(api.request(http.MethodGet, "/api/auth/me", nil, true), 200)
	return api
}
func (a *regressionAPI) request(method, path string, body any, authenticated bool) *httptest.ResponseRecorder {
	a.t.Helper()
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			a.t.Fatal(e)
		}
		reader = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, reader)
	r.Header.Set("Content-Type", "application/json")
	if authenticated {
		r.AddCookie(a.cookie)
	}
	w := httptest.NewRecorder()
	a.server.ServeHTTP(w, r)
	return w
}
func (a *regressionAPI) status(w *httptest.ResponseRecorder, want int) {
	a.t.Helper()
	if w.Code != want {
		a.t.Fatalf("status %d want %d: %s", w.Code, want, w.Body.String())
	}
}
func (a *regressionAPI) json(w *httptest.ResponseRecorder) map[string]any {
	a.t.Helper()
	a.status(w, 200)
	var v map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		a.t.Fatal(e)
	}
	return v
}
func (a *regressionAPI) state() map[string]any {
	a.t.Helper()
	return object(a.json(a.request("GET", "/api/state", nil, true))["project"])
}
func (a *regressionAPI) save(v map[string]any) {
	a.t.Helper()
	a.status(a.request("POST", "/api/project/save", v, true), 200)
}

func TestHTTPLoginProjectsAndBoundaryRoutes(t *testing.T) {
	a := newRegressionAPI(t, false)
	a.status(a.request("GET", "/api/events", nil, false), 401)
	for _, path := range []string{"/logs/server.jsonl", "/logs", "/api/logs/server.jsonl"} {
		a.status(a.request("GET", path, nil, true), 404)
	}
	a.status(a.request("GET", "/api/qr.svg?text=fixture", nil, false), 401)
	qr := a.request("GET", "/api/qr.svg?text="+url.QueryEscape("https://example.test/fixture"), nil, true)
	a.status(qr, 200)
	if !strings.Contains(qr.Header().Get("Content-Type"), "image/svg+xml") || !strings.Contains(qr.Body.String(), "<svg") {
		t.Fatalf("invalid QR: %s", qr.Body.String())
	}
	ret := a.request("GET", "/api/pay/return/epay?out_trade_no=A%26B", nil, false)
	a.status(ret, 302)
	if ret.Header().Get("Location") != "/ui/?pay=return&out_trade_no=A%26B" {
		t.Fatalf("return redirect %q", ret.Header().Get("Location"))
	}
	for _, path := range []string{"/api/pay/create", "/api/pay/packages", "/api/wallet"} {
		a.status(a.request("POST", path, map[string]any{"amount": 1}, true), 503)
	}
	a.status(a.request("POST", "/api/projects/create", map[string]any{"name": "回归项目"}, true), 200)
	listing := a.json(a.request("POST", "/api/projects/list", map[string]any{}, true))
	if len(array(object(listing["list"])["items"])) == 0 {
		t.Fatalf("missing list.items: %v", listing)
	}
	state := a.json(a.request("GET", "/api/state", nil, true))
	if len(array(object(state["projects"])["items"])) == 0 || str(state, "activeProjectId") == "" {
		t.Fatalf("missing state projects.items: %v", state)
	}
}

func TestHTTPOrdinaryUserCannotRunPaidConnectionTests(t *testing.T) {
	a := newRegressionAPI(t, false)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.Error(w, "unexpected upstream", 500) }))
	defer upstream.Close()
	for _, capability := range []string{"TEXT", "IMAGE"} {
		t.Setenv("COMIC_"+capability+"_URL", upstream.URL)
		t.Setenv("COMIC_"+capability+"_KEY", "fixture")
		t.Setenv("COMIC_"+capability+"_MODEL", "fixture")
	}
	for _, path := range []string{"/api/test/text", "/api/test/image", "/api/generate/storyboard", "/api/generate/page"} {
		a.status(a.request("POST", path, map[string]any{}, true), 403)
	}
	if calls.Load() != 0 {
		t.Fatalf("ordinary account dispatched %d upstream calls", calls.Load())
	}
}

func TestHTTPLocalAdminStoryboardImagePersistenceAndZIP(t *testing.T) {
	a := newRegressionAPI(t, true)
	image := []byte("\x89PNG\r\n\x1a\nlocal-http-fixture")
	var textCalls, imageCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("missing local provider credential")
		}
		var payload map[string]any
		if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
			t.Error(e)
			http.Error(w, "bad JSON", 400)
			return
		}
		switch r.URL.Path {
		case "/v1/chat/completions":
			textCalls.Add(1)
			if !strings.Contains(stringJSON(payload), "原文回归标记") {
				t.Error("original text missing from storyboard prompt")
			}
			content := `{"pages":[{"page_number":1,"page_index":1,"original_text_range":"原文回归标记：小红回家。","panels":[{"visual_prompt":"小红回家，走向家门","dialogue_text":[{"text":"你好"}]}]}],"characterLibrary":[{"name":"小红","appearance":"短发"}]}`
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
		case "/v1/images/generations":
			n := imageCalls.Add(1)
			if n == 2 && len(array(payload["image_urls"])) == 0 {
				t.Error("page request lost generated character reference")
			}
			json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(image)}}})
		default:
			t.Errorf("unexpected provider URL %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	a.server.Client.HTTP = provider.Client()
	for _, capability := range []string{"TEXT", "IMAGE"} {
		t.Setenv("COMIC_"+capability+"_URL", provider.URL+"/v1")
		t.Setenv("COMIC_"+capability+"_KEY", "fixture-key")
		t.Setenv("COMIC_"+capability+"_MODEL", "fixture-model")
	}
	generated := a.json(a.request("POST", "/api/generate/storyboard", map[string]any{"originalText": "原文回归标记：小红回家。", "targetPageCount": 1}, true))
	sb := object(generated["storyboard"])
	if len(array(sb["pages"])) != 1 {
		t.Fatalf("storyboard missing: %v", generated)
	}
	a.save(map[string]any{"storyboard": sb, "characterLibrary": sb["characterLibrary"], "originalText": "原文回归标记：小红回家。"})
	char := a.json(a.request("POST", "/api/generate/character", map[string]any{"character": map[string]any{"name": "小红", "appearance": "短发"}}, true))
	charPath := str(char, "imagePath")
	if charPath == "" {
		t.Fatal("character image not returned")
	}
	state := a.state()
	if str(object(array(state["characterLibrary"])[0]), "referenceImagePath") != charPath {
		t.Fatal("character image not persisted")
	}
	page := object(array(object(state["storyboard"])["pages"])[0])
	result := a.json(a.request("POST", "/api/generate/page", map[string]any{"page": page, "storyboard": state["storyboard"]}, true))
	pagePath := str(result, "imagePath")
	if pagePath == "" {
		t.Fatal("page image not returned")
	}
	// A stale frontend save with empty paths must retain server-generated images.
	a.save(map[string]any{"storyboard": map[string]any{"pages": []any{map[string]any{"page_index": 1, "page_image_url": "", "imagePath": ""}}}, "characterLibrary": []any{map[string]any{"name": "小红", "referenceImagePath": ""}}})
	state = a.state()
	page = object(array(object(state["storyboard"])["pages"])[0])
	if str(page, "page_image_url") != pagePath || str(object(array(state["characterLibrary"])[0]), "referenceImagePath") != charPath {
		t.Fatalf("empty save lost images: %v", state)
	}
	files := a.request("GET", "/files?p="+url.QueryEscape(pagePath), nil, true)
	a.status(files, 200)
	if !bytes.Equal(files.Body.Bytes(), image) {
		t.Fatal("served image differs")
	}
	zipped := a.request("GET", "/api/pages/zip", nil, true)
	a.status(zipped, 200)
	zr, e := zip.NewReader(bytes.NewReader(zipped.Body.Bytes()), int64(zipped.Body.Len()))
	if e != nil {
		t.Fatal(e)
	}
	entries := map[string][]byte{}
	for _, f := range zr.File {
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		entries[f.Name] = b
	}
	if !bytes.Equal(entries["comic_page_001.png"], image) || !json.Valid(entries["project.json"]) {
		t.Fatalf("bad ZIP entries: %v", entries)
	}
	if textCalls.Load() != 1 || imageCalls.Load() != 2 {
		t.Fatalf("calls text=%d image=%d", textCalls.Load(), imageCalls.Load())
	}
}

func TestHTTPPersistPageImageIndexFallback(t *testing.T) {
	a := newRegressionAPI(t, true)
	a.save(map[string]any{"storyboard": map[string]any{"pages": []any{map[string]any{"page_index": 1}, map[string]any{"page_index": 2}}}})
	p, e := a.server.Store.ActiveProject(context.Background(), mustRegressionUserID(t, a))
	if e != nil {
		t.Fatal(e)
	}
	if e = a.server.persistImage(context.Background(), p.OwnerID, p.ID, "page", map[string]any{"page": map[string]any{"index": 1}}, "output/index-fallback.png"); e != nil {
		t.Fatalf("request page.index fallback failed: %v", e)
	}
	got := object(array(object(a.state()["storyboard"])["pages"])[0])
	if str(got, "page_image_url") != "output/index-fallback.png" {
		t.Fatalf("image fallback missing: %v", got)
	}
	other := object(array(object(a.state()["storyboard"])["pages"])[1])
	if str(other, "page_image_url") != "" {
		t.Fatalf("fallback modified unrelated page: %v", other)
	}
}
func mustRegressionUserID(t *testing.T, a *regressionAPI) string {
	t.Helper()
	u := object(a.json(a.request("GET", "/api/auth/me", nil, true))["user"])
	return str(u, "id")
}
