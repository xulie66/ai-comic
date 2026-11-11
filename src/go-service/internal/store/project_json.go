package store

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const maxProjectJSON = 16 << 20

func projectJSONRelative(rel string) (string, error) {
	switch rel {
	case "config/settings.json", "projects/asset-library.json":
		return filepath.FromSlash(rel), nil
	default:
		return "", ErrForbidden
	}
}

// LoadProjectJSON returns ErrNotFound for an absent allowed file. It does not
// return an empty fallback, allowing callers to distinguish absent configuration.
func (s *Store) LoadProjectJSON(ctx context.Context, owner, pid, rel string) (json.RawMessage, error) {
	s.fsMu.Lock()
	defer s.fsMu.Unlock()
	relative, err := projectJSONRelative(rel)
	if err != nil {
		return nil, err
	}
	root, err := s.ProjectRoot(ctx, owner, pid)
	if err != nil {
		return nil, err
	}
	if err = noLinks(root, relative); err != nil {
		return nil, err
	}
	h, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer h.Close()
	f, err := h.Open(relative)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxProjectJSON {
		return nil, ErrInvalid
	}
	b, err := io.ReadAll(io.LimitReader(f, maxProjectJSON+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxProjectJSON || !json.Valid(b) {
		return nil, ErrInvalid
	}
	return json.RawMessage(b), nil
}

// SaveProjectJSON serializes writes with state/image operations and atomically
// replaces only a whitelisted project file through its confined directory handle.
func (s *Store) SaveProjectJSON(ctx context.Context, owner, pid, rel string, b json.RawMessage) error {
	if len(b) > maxProjectJSON || !json.Valid(b) {
		return ErrInvalid
	}
	s.fsMu.Lock()
	defer s.fsMu.Unlock()
	relative, err := projectJSONRelative(rel)
	if err != nil {
		return err
	}
	root, err := s.ProjectRoot(ctx, owner, pid)
	if err != nil {
		return err
	}
	if err = noLinks(root, relative); err != nil {
		return err
	}
	h, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer h.Close()
	if err = h.MkdirAll(filepath.Dir(relative), 0700); err != nil {
		return err
	}
	return writeRoot(h, relative, b)
}
