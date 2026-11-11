package httpapi

import (
	"archive/zip"
	"comic.local/go-service/internal/jobs"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"comic.local/go-service/internal/auth"
	"comic.local/go-service/internal/generation"
	"comic.local/go-service/internal/operations"
	"comic.local/go-service/internal/store"
)

type Server struct {
	Store            *store.Store
	Auth             *auth.Service
	Ops              *operations.Service
	Client           *generation.Client
	Logger           *slog.Logger
	PublicDir        string
	MaxBody          int64
	Jobs             *jobs.Manager
	generationLimits generationLimitState
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	if r.Method != "GET" && r.Method != "HEAD" && r.URL.Path != "/api/pay/notify/epay" {
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, e := url.Parse(origin)
			if e != nil || parsed.Host != r.Host {
				s.fail(w, 403, "请求来源校验失败")
				return
			}
		}
	}
	if r.URL.Path == "/api/health" {
		s.json(w, 200, map[string]any{"ok": true, "version": "3.0.0-go", "runtime": "go"})
		return
	}
	if s.Ops != nil && (r.URL.Path == "/api/pay/notify/epay" || r.URL.Path == "/api/pay/return/epay") {
		s.Ops.ServeHTTP(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/auth/") {
		s.authentication(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/files" && !strings.HasPrefix(r.URL.Path, "/files/") && r.URL.Path != "/download" {
		s.static(w, r)
		return
	}
	u, err := s.Auth.Authenticate(r)
	if err != nil {
		s.fail(w, 401, "请先登录")
		return
	}
	r = r.WithContext(auth.WithUser(r.Context(), u))
	if strings.HasPrefix(r.URL.Path, "/api/logs/") || r.URL.Path == "/logs" {
		s.fail(w, 404, "页面不存在")
		return
	}
	if s.Ops != nil && (r.URL.Path == "/api/qr.svg" || strings.HasPrefix(r.URL.Path, "/api/admin/") || strings.HasPrefix(r.URL.Path, "/api/pay/") || strings.HasPrefix(r.URL.Path, "/api/models/") || r.URL.Path == "/api/credits" || r.URL.Path == "/api/wallet" || r.URL.Path == "/api/redeem") {
		s.Ops.ServeHTTP(w, r.WithContext(operations.WithIdentity(r.Context(), u.ID, u.IsAdmin)))
		return
	}
	if err = s.business(w, r, u); err != nil {
		s.Logger.Error("request failed", "path", r.URL.Path, "user", u.ID, "error", err)
		if errors.Is(err, ErrLimit) {
			s.fail(w, 429, "请求频率或今日额度已达上限")
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			s.fail(w, 404, "项目或文件不存在")
		} else if errors.Is(err, store.ErrForbidden) {
			s.fail(w, 403, "无访问权限")
		} else if errors.Is(err, store.ErrInvalid) {
			s.fail(w, 400, "请求内容有误")
		} else {
			s.fail(w, 500, "处理失败，详细信息已记录在服务端")
		}
	}
}
func (s *Server) json(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) fail(w http.ResponseWriter, status int, msg string) {
	s.json(w, status, map[string]any{"success": false, "error": msg})
}
func (s *Server) body(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	n := s.MaxBody
	if n == 0 {
		n = 64 << 20
	}
	var p map[string]any
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, n)).Decode(&p)
	if err == io.EOF {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, store.ErrInvalid
	}
	if r.Method != "GET" && r.Method != "HEAD" && r.Method != "POST" {
		return nil, store.ErrInvalid
	}
	return p, nil
}
func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }
func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func array(v any) []any { a, _ := v.([]any); return a }
func (s *Server) authentication(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/api/auth/me" || path == "/api/auth/session" {
		u, e := s.Auth.Authenticate(r)
		if e != nil {
			s.fail(w, 401, "请先登录")
			return
		}
		token, _ := s.Auth.Token(r)
		s.Auth.SetCookie(w, auth.Result{User: u, Token: token, ExpiresAt: time.Now().Add(30 * 24 * time.Hour).UnixMilli()})
		s.json(w, 200, map[string]any{"success": true, "user": u})
		return
	}
	if r.Method != "POST" {
		s.fail(w, 405, "请求方法有误")
		return
	}
	p, e := s.body(w, r)
	if e != nil {
		s.fail(w, 400, "请求内容有误")
		return
	}
	if path == "/api/auth/logout" {
		token, _ := s.Auth.Token(r)
		_ = s.Auth.Logout(r.Context(), token)
		s.Auth.ClearCookie(w)
		s.json(w, 200, map[string]any{"success": true})
		return
	}
	if path == "/api/auth/password" {
		u, e := s.Auth.Authenticate(r)
		if e != nil {
			s.fail(w, 401, "请先登录")
			return
		}
		result, e := s.Auth.ChangePassword(r.Context(), u.ID, str(p, "oldPassword"), str(p, "newPassword"))
		if e != nil {
			s.fail(w, 400, "密码校验失败或新密码不符合要求")
			return
		}
		s.Auth.SetCookie(w, result)
		s.json(w, 200, map[string]any{"success": true, "token": result.Token, "user": result.User})
		return
	}
	c := auth.Credentials{Username: str(p, "username"), Password: str(p, "password"), DisplayName: str(p, "displayName")}
	var result auth.Result
	switch path {
	case "/api/auth/login":
		result, e = s.Auth.Login(r.Context(), c)
	case "/api/auth/register":
		if s.Ops == nil || s.Store.Pool() == nil {
			s.fail(w, 403, "当前未开放注册")
			return
		}
		policy, err := s.Ops.RegistrationPolicy(r.Context())
		if err != nil || !policy.RegisterOpen {
			s.fail(w, 403, "当前未开放注册")
			return
		}
		if policy.InviteRequired && (policy.InviteCode == "" || str(p, "inviteCode") != policy.InviteCode) {
			s.fail(w, 403, "邀请码校验失败")
			return
		}
		result, e = s.Auth.Register(r.Context(), c)
	default:
		s.fail(w, 404, "接口不存在")
		return
	}
	if e != nil {
		s.Logger.Warn("authentication failed", "path", path, "error", e)
		s.fail(w, 400, "账号或密码有误，或注册条件未满足")
		return
	}
	s.Auth.SetCookie(w, result)
	s.json(w, 200, map[string]any{"success": true, "token": result.Token, "user": result.User})
}
func (s *Server) active(ctx context.Context, owner string) (store.Project, error) {
	p, e := s.Store.ActiveProject(ctx, owner)
	if errors.Is(e, store.ErrNotFound) {
		list, le := s.Store.ListProjects(ctx, owner)
		if le != nil {
			return p, le
		}
		if len(list) > 0 {
			p = list[0]
		} else {
			p, e = s.Store.CreateProject(ctx, owner, "未命名项目")
			if e != nil {
				return p, e
			}
		}
		e = s.Store.SetActiveProject(ctx, owner, p.ID)
	}
	return p, e
}
func (s *Server) readState(ctx context.Context, uid, pid string) (map[string]any, error) {
	b, e := s.Store.LoadState(ctx, uid, pid)
	if e != nil {
		return nil, e
	}
	var p map[string]any
	if json.Unmarshal(b, &p) != nil || p == nil {
		p = map[string]any{}
	}
	return p, nil
}
func (s *Server) saveState(ctx context.Context, uid, pid string, p map[string]any) error {
	b, e := json.Marshal(p)
	if e != nil {
		return e
	}
	return s.Store.SaveState(ctx, uid, pid, b)
}
func (s *Server) business(w http.ResponseWriter, r *http.Request, u store.PublicUser) error {
	ctx := r.Context()
	path := r.URL.Path
	if path == "/files" || strings.HasPrefix(path, "/files/") || path == "/download" {
		pid := r.URL.Query().Get("projectId")
		if pid == "" {
			p, e := s.active(ctx, u.ID)
			if e != nil {
				return e
			}
			pid = p.ID
		}
		name := r.URL.Query().Get("p")
		if name == "" && strings.HasPrefix(path, "/files/") {
			name = strings.TrimPrefix(path, "/files/")
		}
		b, e := s.Store.ReadImage(ctx, u.ID, pid, name)
		if e != nil {
			return e
		}
		w.Header().Set("Content-Type", http.DetectContentType(b))
		if path == "/download" {
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)}))
		}
		http.ServeContent(w, r, filepath.Base(name), time.Time{}, strings.NewReader(string(b)))
		return nil
	}
	if strings.HasPrefix(path, "/api/projects/") {
		p, e := s.body(w, r)
		if e != nil {
			return e
		}
		var project store.Project
		switch strings.TrimPrefix(path, "/api/projects/") {
		case "list":
		case "create":
			project, e = s.Store.CreateProject(ctx, u.ID, str(p, "name"))
			if e == nil {
				e = s.Store.SetActiveProject(ctx, u.ID, project.ID)
			}
		case "open":
			project, e = s.Store.GetProject(ctx, u.ID, str(p, "id"))
			if e == nil {
				e = s.Store.SetActiveProject(ctx, u.ID, project.ID)
			}
		case "rename":
			project, e = s.Store.RenameProject(ctx, u.ID, str(p, "id"), str(p, "name"))
		case "delete":
			e = s.Store.DeleteProject(ctx, u.ID, str(p, "id"))
		default:
			return store.ErrNotFound
		}
		if e != nil {
			return e
		}
		list, e := s.Store.ListProjects(ctx, u.ID)
		if e != nil {
			return e
		}
		active, _ := s.active(ctx, u.ID)
		items, e := s.Store.SummarizeProjects(ctx, u.ID, list)
		if e != nil {
			return e
		}
		s.json(w, 200, map[string]any{"success": true, "list": map[string]any{"items": items, "activeId": active.ID}, "project": project})
		return nil
	}
	if path == "/api/events" {
		return s.events(w, r, u.ID)
	}
	if strings.HasPrefix(path, "/api/render/") {
		return s.render(w, r, u)
	}
	project, e := s.active(ctx, u.ID)
	if e != nil {
		return e
	}
	if handled, err := s.archive(w, r, u, project.ID); handled {
		return err
	}
	if handled, err := s.compat(w, r, u, project.ID); handled {
		return err
	}
	if path == "/api/extract/saved" || path == "/api/extract/save" {
		return s.assets(w, r, u, project.ID)
	}
	if strings.HasPrefix(path, "/api/test/") {
		return s.tests(w, r, u, project.ID)
	}
	if path == "/api/state" {
		p, e := s.readState(ctx, u.ID, project.ID)
		if e != nil {
			return e
		}
		settings, e := s.loadSettings(ctx, u.ID, project.ID)
		if e != nil {
			return e
		}
		list, e := s.Store.ListProjects(ctx, u.ID)
		if e != nil {
			return e
		}
		items, e := s.Store.SummarizeProjects(ctx, u.ID, list)
		if e != nil {
			return e
		}
		s.json(w, 200, map[string]any{"success": true, "project": p, "settings": publicSettings(settings), "projects": map[string]any{"items": items, "activeId": project.ID, "activeRoot": ""}, "render": s.renderSnapshot(u.ID, project.ID), "activeProjectId": project.ID, "version": "3.0.0-go", "activeProject": project, "user": u})
		return nil
	}
	if path == "/api/upload" {
		return s.upload(w, r, u.ID, project.ID)
	}
	if path == "/api/usage" {
		s.json(w, 200, map[string]any{"success": true, "quota": map[string]any{"isAdmin": u.IsAdmin, "unlimited": u.IsAdmin, "dailyPageQuota": u.DailyPageQuota}, "usage": u.Usage})
		return nil
	}
	p, e := s.body(w, r)
	if e != nil {
		return e
	}
	switch path {
	case "/api/project/save":
		e = s.saveState(ctx, u.ID, project.ID, p)
	case "/api/project/import":
		var np store.Project
		np, e = s.Store.CreateProject(ctx, u.ID, first(str(p, "name"), "导入项目"))
		if e == nil {
			e = s.saveState(ctx, u.ID, np.ID, object(p["project"]))
		}
		if e == nil {
			e = s.Store.SetActiveProject(ctx, u.ID, np.ID)
		}
		if e != nil {
			return e
		}
		list, _ := s.Store.ListProjects(ctx, u.ID)
		s.json(w, 200, map[string]any{"success": true, "project": np, "list": map[string]any{"items": list, "activeId": np.ID}})
		return nil
	case "/api/settings/save":
		e = s.saveSettings(ctx, u.ID, project.ID, p)
	case "/api/preview/prompt":
		kind := str(p, "kind")
		prompt := generation.PagePrompt(p)
		if kind == "character" {
			prompt = generation.CharacterPrompt(p)
		}
		if kind == "scene" {
			prompt = generation.ScenePrompt(p)
		}
		s.json(w, 200, map[string]any{"success": true, "prompt": prompt, "length": len([]rune(prompt))})
		return nil
	case "/api/generate/storyboard", "/api/generate/page", "/api/generate/character", "/api/generate/scene", "/api/generate/rewritePrompt", "/api/extract/assets":
		delete(p, "_jobTaskRef")
		result, e := s.generate(ctx, u, project.ID, strings.TrimPrefix(path, "/api/generate/"), p)
		if e != nil {
			return e
		}
		s.json(w, 200, result)
		return nil
	case "/api/images/choose", "/api/settings/chooseOutputDir":
		s.json(w, 200, map[string]any{"success": true, "canceled": true})
		return nil
	default:
		return store.ErrNotFound
	}
	if e != nil {
		return e
	}
	s.json(w, 200, map[string]any{"success": true})
	return nil
}
func first(v, d string) string {
	if v != "" {
		return v
	}
	return d
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		if s.PublicDir == "" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(s.PublicDir, "ui", "index.html"))
		return
	}
	if strings.HasPrefix(r.URL.Path, "/legacy") || strings.HasPrefix(r.URL.Path, "/logs") || strings.Contains(r.URL.Path, "..") || (!strings.HasPrefix(r.URL.Path, "/ui/") && !strings.HasPrefix(r.URL.Path, "/admin/")) {
		http.NotFound(w, r)
		return
	}
	if s.PublicDir == "" {
		http.NotFound(w, r)
		return
	}
	http.FileServer(http.Dir(s.PublicDir)).ServeHTTP(w, r)
}
func (s *Server) exportZIP(w http.ResponseWriter, r *http.Request, uid, pid string) error {
	p, e := s.readState(r.Context(), uid, pid)
	if e != nil {
		return e
	}
	type entry struct {
		Name string
		Data []byte
	}
	entries := []entry{}
	for i, pv := range array(object(p["storyboard"])["pages"]) {
		page := object(pv)
		name := pageImage(page)
		if name == "" {
			continue
		}
		b, e := s.Store.ReadImage(r.Context(), uid, pid, name)
		if e != nil {
			return e
		}
		entries = append(entries, entry{fmt.Sprintf("page_%03d.png", i+1), b})
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="comic.zip"`)
	z := zip.NewWriter(w)
	defer z.Close()
	for _, v := range entries {
		f, e := z.Create(v.Name)
		if e != nil {
			return e
		}
		if _, e = f.Write(v.Data); e != nil {
			return e
		}
	}
	raw, _ := json.Marshal(p)
	f, e := z.Create("project.json")
	if e != nil {
		return e
	}
	_, e = f.Write(raw)
	return e
}
func (s *Server) upload(w http.ResponseWriter, r *http.Request, uid, pid string) error {
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	if e := r.ParseMultipartForm(16 << 20); e != nil {
		return store.ErrInvalid
	}
	defer r.MultipartForm.RemoveAll()
	out := []map[string]any{}
	for _, parts := range r.MultipartForm.File {
		for _, part := range parts {
			f, e := part.Open()
			if e != nil {
				return e
			}
			b, e := io.ReadAll(io.LimitReader(f, 32<<20))
			f.Close()
			if e != nil {
				return e
			}
			if !strings.HasPrefix(http.DetectContentType(b), "image/") {
				return store.ErrInvalid
			}
			name := "references/upload/" + store.NewID("ref") + filepath.Ext(filepath.Base(part.Filename))
			saved, e := s.Store.WriteImage(r.Context(), uid, pid, name, b)
			if e != nil {
				return e
			}
			out = append(out, map[string]any{"path": saved, "name": part.Filename})
		}
	}
	s.json(w, 200, map[string]any{"success": true, "files": out})
	return nil
}

var _ = os.ErrNotExist
