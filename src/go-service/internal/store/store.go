package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrInvalid   = errors.New("invalid input")
	ErrForbidden = errors.New("forbidden")
)

type User struct {
	ID                  string     `json:"id"`
	Username            string     `json:"username"`
	DisplayName         string     `json:"display_name"`
	Salt                string     `json:"salt"`
	Hash                string     `json:"hash"`
	IsAdmin             bool       `json:"is_admin"`
	CreatedAt           time.Time  `json:"created_at"`
	LastLoginAt         *time.Time `json:"last_login_at"`
	DailyPageQuota      int        `json:"daily_page_quota"`
	Credits             float64    `json:"credits"`
	CreditStatus        string     `json:"credit_status"`
	DefaultImageModelID string     `json:"default_image_model_id"`
	DefaultTextModelID  string     `json:"default_text_model_id"`
	UsagePages          int64      `json:"usage_pages"`
	UsageImages         int64      `json:"usage_images"`
	UsageStoryboards    int64      `json:"usage_storyboards"`
	UsageCharacters     int64      `json:"usage_characters"`
}
type PublicUser struct {
	ID                  string           `json:"id"`
	Username            string           `json:"username"`
	DisplayName         string           `json:"displayName"`
	IsAdmin             bool             `json:"isAdmin"`
	CreatedAt           time.Time        `json:"createdAt"`
	LastLoginAt         *time.Time       `json:"lastLoginAt"`
	DailyPageQuota      int              `json:"dailyPageQuota"`
	Credits             float64          `json:"credits"`
	Status              string           `json:"status"`
	DefaultImageModelID string           `json:"defaultImageModelId"`
	DefaultTextModelID  string           `json:"defaultTextModelId"`
	Usage               map[string]int64 `json:"usage"`
}

func (u User) Public() PublicUser {
	status := u.CreditStatus
	if status == "" {
		status = "active"
	}
	return PublicUser{u.ID, u.Username, u.DisplayName, u.IsAdmin, u.CreatedAt, u.LastLoginAt, u.DailyPageQuota, u.Credits, status, u.DefaultImageModelID, u.DefaultTextModelID, map[string]int64{"pages": u.UsagePages, "images": u.UsageImages, "storyboards": u.UsageStoryboards, "characters": u.UsageCharacters}}
}

type Session struct {
	Token     string `json:"token"`
	UserID    string `json:"user_id"`
	ExpiresAt int64  `json:"expires_at"`
}
type Project struct {
	ID        string     `json:"id"`
	OwnerID   string     `json:"ownerId"`
	Name      string     `json:"name"`
	Root      string     `json:"root"`
	Legacy    bool       `json:"legacy"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt"`
}
type Options struct {
	DataRoot, DatabaseURL string
	SkipSchema            bool
}
type snapshot struct {
	Users    map[string]User    `json:"users"`
	Sessions map[string]Session `json:"sessions"`
	Projects map[string]Project `json:"projects"`
	Active   map[string]string  `json:"active"`
}
type Store struct {
	mu         sync.RWMutex
	fsMu       sync.Mutex
	root, file string
	data       snapshot
	pool       *pgxpool.Pool
}

func Open(ctx context.Context, o Options) (*Store, error) {
	root, err := filepath.Abs(o.DataRoot)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, file: filepath.Join(root, "go-store.json"), data: snapshot{map[string]User{}, map[string]Session{}, map[string]Project{}, map[string]string{}}}
	if o.DatabaseURL != "" {
		s.pool, err = pgxpool.New(ctx, o.DatabaseURL)
		if err != nil {
			return nil, err
		}
		if o.SkipSchema {
			err = s.pool.Ping(ctx)
		} else {
			err = s.initPG(ctx)
		}
		if err != nil {
			s.pool.Close()
			return nil, err
		}
		return s, nil
	}
	b, err := os.ReadFile(s.file)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	if s.data.Users == nil || s.data.Sessions == nil || s.data.Projects == nil || s.data.Active == nil {
		return nil, ErrInvalid
	}
	return s, nil
}
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}
func (s *Store) Pool() *pgxpool.Pool { return s.pool }
func (s *Store) DataRoot() string    { return s.root }
func NewID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
func atomicWrite(file string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), ".go-write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	return os.Rename(name, file)
}

// mutate publishes local changes only after the durable atomic replacement succeeds.
func (s *Store) mutate(fn func(*snapshot) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	var next snapshot
	if err = json.Unmarshal(b, &next); err != nil {
		return err
	}
	if err = fn(&next); err != nil {
		return err
	}
	b, err = json.Marshal(next)
	if err != nil {
		return err
	}
	if err = atomicWrite(s.file, b); err != nil {
		return err
	}
	s.data = next
	return nil
}
func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	if s.pool != nil {
		return s.pgUser(ctx, "id", id)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.data.Users[id]
	if !ok {
		return u, ErrNotFound
	}
	return u, nil
}
func (s *Store) UserByName(ctx context.Context, name string) (User, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if s.pool != nil {
		return s.pgUser(ctx, "username", name)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.data.Users {
		if u.Username == name {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}
func (s *Store) CreateUser(ctx context.Context, u User) error {
	if s.pool != nil {
		return s.pgCreateUser(ctx, u)
	}
	return s.mutate(func(d *snapshot) error {
		for _, v := range d.Users {
			if v.Username == u.Username || v.ID == u.ID {
				return ErrConflict
			}
		}
		d.Users[u.ID] = u
		return nil
	})
}
func (s *Store) TouchLogin(ctx context.Context, id string) error {
	now := time.Now().UTC()
	if s.pool != nil {
		_, err := s.pool.Exec(ctx, "UPDATE users SET last_login_at=$2 WHERE id=$1", id, now)
		return err
	}
	return s.mutate(func(d *snapshot) error {
		u, ok := d.Users[id]
		if !ok {
			return ErrNotFound
		}
		u.LastLoginAt = &now
		d.Users[id] = u
		return nil
	})
}
func (s *Store) PutSession(ctx context.Context, v Session) error {
	if s.pool != nil {
		_, err := s.pool.Exec(ctx, "INSERT INTO sessions(token,user_id,expires_at) VALUES($1,$2,$3)", v.Token, v.UserID, v.ExpiresAt)
		return pgError(err)
	}
	return s.mutate(func(d *snapshot) error {
		if _, ok := d.Users[v.UserID]; !ok {
			return ErrNotFound
		}
		if _, ok := d.Sessions[v.Token]; ok {
			return ErrConflict
		}
		for k, v := range d.Sessions {
			if v.ExpiresAt <= time.Now().UnixMilli() {
				delete(d.Sessions, k)
			}
		}
		d.Sessions[v.Token] = v
		return nil
	})
}
func (s *Store) GetSession(ctx context.Context, token string) (Session, error) {
	var v Session
	if s.pool != nil {
		err := s.pool.QueryRow(ctx, "SELECT token,user_id,expires_at FROM sessions WHERE token=$1 AND expires_at>$2", token, time.Now().UnixMilli()).Scan(&v.Token, &v.UserID, &v.ExpiresAt)
		return v, pgError(err)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data.Sessions[token]
	if !ok || v.ExpiresAt <= time.Now().UnixMilli() {
		return Session{}, ErrNotFound
	}
	return v, nil
}
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	if s.pool != nil {
		_, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token=$1", token)
		return err
	}
	return s.mutate(func(d *snapshot) error { delete(d.Sessions, token); return nil })
}
func (s *Store) ChangePassword(ctx context.Context, id, salt, hash string) error {
	if s.pool != nil {
		return s.pgChangePassword(ctx, id, salt, hash)
	}
	return s.mutate(func(d *snapshot) error {
		u, ok := d.Users[id]
		if !ok {
			return ErrNotFound
		}
		u.Salt = salt
		u.Hash = hash
		d.Users[id] = u
		for k, v := range d.Sessions {
			if v.UserID == id {
				delete(d.Sessions, k)
			}
		}
		return nil
	})
}
func (s *Store) ListProjects(ctx context.Context, owner string) ([]Project, error) {
	if owner == "" {
		return nil, ErrForbidden
	}
	if s.pool != nil {
		return s.pgListProjects(ctx, owner)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Project{}
	for _, p := range s.data.Projects {
		if p.OwnerID == owner {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (s *Store) GetProject(ctx context.Context, owner, id string) (Project, error) {
	if owner == "" {
		return Project{}, ErrForbidden
	}
	if s.pool != nil {
		return s.pgProject(ctx, owner, id)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.data.Projects[id]
	if !ok || p.OwnerID != owner {
		return Project{}, ErrNotFound
	}
	return p, nil
}
func validName(name string) bool { return strings.TrimSpace(name) != "" && len([]rune(name)) <= 120 }
func (s *Store) CreateProject(ctx context.Context, owner, name string) (Project, error) {
	if !validName(name) {
		return Project{}, ErrInvalid
	}
	if _, err := s.UserByID(ctx, owner); err != nil {
		return Project{}, err
	}
	p := Project{ID: NewID("p"), OwnerID: owner, Name: strings.TrimSpace(name), CreatedAt: time.Now().UTC()}
	p.Root = "projects/" + p.ID
	var err error
	if s.pool != nil {
		err = s.pgInsertProject(ctx, p)
	} else {
		err = s.mutate(func(d *snapshot) error { d.Projects[p.ID] = p; return nil })
	}
	return p, err
}
func (s *Store) RenameProject(ctx context.Context, owner, id, name string) (Project, error) {
	if !validName(name) {
		return Project{}, ErrInvalid
	}
	if s.pool != nil {
		return s.pgRenameProject(ctx, owner, id, name)
	}
	var p Project
	err := s.mutate(func(d *snapshot) error {
		var ok bool
		p, ok = d.Projects[id]
		if !ok || p.OwnerID != owner || owner == "" {
			return ErrNotFound
		}
		now := time.Now().UTC()
		p.Name = strings.TrimSpace(name)
		p.UpdatedAt = &now
		d.Projects[id] = p
		return nil
	})
	return p, err
}
func (s *Store) DeleteProject(ctx context.Context, owner, id string) error {
	s.fsMu.Lock()
	defer s.fsMu.Unlock()
	if s.pool != nil {
		return s.pgDeleteProject(ctx, owner, id)
	}
	return s.mutate(func(d *snapshot) error {
		p, ok := d.Projects[id]
		if !ok || p.OwnerID != owner || owner == "" {
			return ErrNotFound
		}
		delete(d.Projects, id)
		for u, pid := range d.Active {
			if pid == id {
				delete(d.Active, u)
			}
		}
		return nil
	})
}
func (s *Store) ActiveProject(ctx context.Context, owner string) (Project, error) {
	if s.pool != nil {
		return s.pgActive(ctx, owner)
	}
	s.mu.RLock()
	id := s.data.Active[owner]
	s.mu.RUnlock()
	return s.GetProject(ctx, owner, id)
}
func (s *Store) SetActiveProject(ctx context.Context, owner, id string) error {
	if s.pool != nil {
		return s.pgSetActive(ctx, owner, id)
	}
	return s.mutate(func(d *snapshot) error {
		p, ok := d.Projects[id]
		if !ok || p.OwnerID != owner || owner == "" {
			return ErrNotFound
		}
		d.Active[owner] = id
		return nil
	})
}
