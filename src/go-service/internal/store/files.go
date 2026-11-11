package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ProjectRoot only accepts the canonical tenant directory. Legacy root='.' must be
// migrated explicitly; exposing that shared root would expose other users' data.
func (s *Store) ProjectRoot(ctx context.Context, owner, id string) (string, error) {
	p, err := s.GetProject(ctx, owner, id)
	if err != nil {
		return "", err
	}
	if id == "" || strings.ContainsAny(id, "/\\:") || id == "." || id == ".." {
		return "", ErrForbidden
	}
	expected := filepath.Join("projects", id)
	if filepath.Clean(p.Root) != expected {
		return "", ErrForbidden
	}
	root := filepath.Join(s.root, expected)
	for _, dir := range []string{filepath.Join(s.root, "projects"), root} {
		if err = os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
		st, err := os.Lstat(dir)
		if err != nil {
			return "", err
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return "", ErrForbidden
		}
	}
	return root, nil
}
func cleanRelative(rel string) (string, error) {
	if rel == "" || strings.ContainsAny(rel, "\\:\x00") || strings.HasPrefix(rel, "/") {
		return "", ErrForbidden
	}
	for _, p := range strings.Split(rel, "/") {
		if p == ".." || p == "." || p == "" {
			return "", ErrForbidden
		}
	}
	return filepath.FromSlash(rel), nil
}
func (s *Store) imageRelative(ctx context.Context, owner, id, rel string) (string, error) {
	root, err := s.ProjectRoot(ctx, owner, id)
	if err != nil {
		return "", err
	}
	normalized := strings.ReplaceAll(rel, "\\", "/")
	prefix := strings.ReplaceAll(root, "\\", "/") + "/"
	if strings.HasPrefix(normalized, prefix) {
		rel = strings.TrimPrefix(normalized, prefix)
	} else if strings.HasPrefix(normalized, "/data/projects/"+id+"/") {
		rel = strings.TrimPrefix(normalized, "/data/projects/"+id+"/")
	}
	r, err := cleanRelative(rel)
	if err != nil {
		return "", err
	}
	slash := filepath.ToSlash(r)
	if !strings.HasPrefix(slash, "output/") && !strings.HasPrefix(slash, "references/") {
		return "", ErrForbidden
	}
	switch strings.ToLower(filepath.Ext(r)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return r, nil
	}
	return "", ErrInvalid
}
func noLinks(root, rel string) error {
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		st, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return ErrForbidden
		}
	}
	return nil
}
func (s *Store) ImagePath(ctx context.Context, owner, id, relative string) (string, error) {
	r, err := s.imageRelative(ctx, owner, id, relative)
	if err != nil {
		return "", err
	}
	root, err := s.ProjectRoot(ctx, owner, id)
	if err != nil {
		return "", err
	}
	if err = noLinks(root, r); err != nil {
		return "", err
	}
	return filepath.Join(root, r), nil
}

// ReadImage uses os.Root so concurrent symlink replacement cannot escape the tenant.
func (s *Store) ReadImage(ctx context.Context, owner, id, relative string) ([]byte, error) {
	s.fsMu.Lock()
	defer s.fsMu.Unlock()
	r, err := s.imageRelative(ctx, owner, id, relative)
	if err != nil {
		return nil, err
	}
	root, err := s.ProjectRoot(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if err = noLinks(root, r); err != nil {
		return nil, err
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	b, err := handle.ReadFile(r)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}
func (s *Store) WriteImage(ctx context.Context, owner, id, relative string, b []byte) (string, error) {
	s.fsMu.Lock()
	defer s.fsMu.Unlock()
	r, err := s.imageRelative(ctx, owner, id, relative)
	if err != nil {
		return "", err
	}
	root, err := s.ProjectRoot(ctx, owner, id)
	if err != nil {
		return "", err
	}
	if err = noLinks(root, r); err != nil {
		return "", err
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	if err = handle.MkdirAll(filepath.Dir(r), 0700); err != nil {
		return "", err
	}
	if err = writeRoot(handle, r, b); err != nil {
		return "", err
	}
	return filepath.ToSlash(r), nil
}
func writeRoot(root *os.Root, rel string, b []byte) error {
	tmp := filepath.Join(filepath.Dir(rel), NewID(".tmp"))
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, err = f.Write(b)
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
	return root.Rename(tmp, rel)
}
func (s *Store) LoadState(ctx context.Context, owner, id string) (json.RawMessage, error) {
	s.fsMu.Lock()
	defer s.fsMu.Unlock()
	return s.loadStateLocked(ctx, owner, id)
}
func (s *Store) loadStateLocked(ctx context.Context, owner, id string) (json.RawMessage, error) {
	root, err := s.ProjectRoot(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	h, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer h.Close()
	b, err := h.ReadFile("projects/recent-project.json")
	if errors.Is(err, os.ErrNotExist) {
		return json.RawMessage(`{}`), nil
	}
	if err != nil {
		return nil, err
	}
	if !json.Valid(b) {
		return nil, ErrInvalid
	}
	return b, nil
}
func (s *Store) SaveState(ctx context.Context, owner, id string, b json.RawMessage) error {
	if !json.Valid(b) || len(b) > 32<<20 {
		return ErrInvalid
	}
	s.fsMu.Lock()
	defer s.fsMu.Unlock()
	var incoming map[string]any
	if err := json.Unmarshal(b, &incoming); err != nil || incoming == nil {
		return ErrInvalid
	}
	old, err := s.loadStateLocked(ctx, owner, id)
	if err != nil {
		return err
	}
	var previous map[string]any
	if err = json.Unmarshal(old, &previous); err != nil {
		return err
	}
	preserveImages(previous, incoming)
	b, err = json.Marshal(incoming)
	if err != nil {
		return err
	}
	return s.writeStateLocked(ctx, owner, id, b)
}
func (s *Store) writeStateLocked(ctx context.Context, owner, id string, b []byte) error {
	root, err := s.ProjectRoot(ctx, owner, id)
	if err != nil {
		return err
	}
	h, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer h.Close()
	if err = h.MkdirAll("projects", 0700); err != nil {
		return err
	}
	return writeRoot(h, filepath.Join("projects", "recent-project.json"), b)
}
