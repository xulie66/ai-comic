package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MigrationPlan struct {
	ProjectID string   `json:"projectId"`
	OwnerID   string   `json:"ownerId"`
	OldRoot   string   `json:"oldRoot"`
	NewRoot   string   `json:"newRoot"`
	Paths     []string `json:"paths"`
	Backup    string   `json:"backup,omitempty"`
	Status    string   `json:"status"`
}

var migrationPaths = []string{"projects/recent-project.json", "projects/asset-library.json", "config", "output", "references", "assets", "logs"}

func (s *Store) LegacyMigrationPlans(ctx context.Context) ([]MigrationPlan, error) {
	out := []MigrationPlan{}
	var all []Project
	if s.pool != nil {
		rows, err := s.pool.Query(ctx, "SELECT "+projectCols+" FROM projects WHERE root='.' ORDER BY id")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			p, e := scanProject(rows)
			if e != nil {
				return nil, e
			}
			all = append(all, p)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	} else {
		s.mu.RLock()
		for _, p := range s.data.Projects {
			if p.Root == "." {
				all = append(all, p)
			}
		}
		s.mu.RUnlock()
	}
	for _, p := range all {
		if p.ID == "" || strings.ContainsAny(p.ID, "/\\:") || p.ID == "." || p.ID == ".." {
			return nil, ErrInvalid
		}
		plan := MigrationPlan{ProjectID: p.ID, OwnerID: p.OwnerID, OldRoot: ".", NewRoot: "projects/" + p.ID, Status: "planned"}
		for _, rel := range migrationPaths {
			_, err := os.Lstat(filepath.Join(s.root, filepath.FromSlash(rel)))
			if err == nil {
				plan.Paths = append(plan.Paths, rel)
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
		out = append(out, plan)
	}
	return out, nil
}
func copyMigrationTree(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("migration refuses symlink: %s", src)
	}
	if st.IsDir() {
		if err = os.Mkdir(dst, 0700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err = copyMigrationTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("migration refuses special file: %s", src)
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	ce := out.Close()
	if err != nil {
		return err
	}
	return ce
}
func migrationRewrite(v any, oldRoot, newRoot string) any {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			x[k] = migrationRewrite(item, oldRoot, newRoot)
		}
	case []any:
		for i, item := range x {
			x[i] = migrationRewrite(item, oldRoot, newRoot)
		}
	case string:
		normalized := strings.ReplaceAll(x, "\\", "/")
		for _, prefix := range []string{strings.ReplaceAll(oldRoot, "\\", "/") + "/", "/data/"} {
			if strings.HasPrefix(normalized, prefix) {
				rel := strings.TrimPrefix(normalized, prefix)
				allowed := false
				for _, dir := range []string{"output/", "references/", "assets/"} {
					if strings.HasPrefix(rel, dir) {
						allowed = true
					}
				}
				if allowed {
					clean, e := cleanRelative(rel)
					if e == nil {
						return filepath.ToSlash(filepath.Join(newRoot, clean))
					}
				}
			}
		}
	}
	return v
}

// ApplyLegacyMigration must run with all service processes stopped. Metadata is
// committed last; originals and backups are retained even after a partial error.
func (s *Store) ApplyLegacyMigration(ctx context.Context, id string) (MigrationPlan, error) {
	s.fsMu.Lock()
	defer s.fsMu.Unlock()
	plans, err := s.LegacyMigrationPlans(ctx)
	if err != nil {
		return MigrationPlan{}, err
	}
	var p MigrationPlan
	found := false
	for _, plan := range plans {
		if plan.ProjectID == id {
			p = plan
			found = true
		}
	}
	if !found {
		return p, ErrNotFound
	}
	if len(p.Paths) == 0 {
		return p, fmt.Errorf("legacy project has no source files")
	}
	target := filepath.Join(s.root, filepath.FromSlash(p.NewRoot))
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return p, fmt.Errorf("migration target already exists")
	}
	if err = noLinks(s.root, filepath.Join("projects", id)); err != nil {
		return p, err
	}
	backup := filepath.Join(s.root, "migration-backups", time.Now().UTC().Format("20060102T150405.000000000")+"-"+id)
	if err = os.MkdirAll(backup, 0700); err != nil {
		return p, err
	}
	p.Backup = backup
	p.Status = "backing_up"
	manifest := filepath.Join(backup, "manifest.json")
	save := func() error {
		b, e := json.MarshalIndent(p, "", "  ")
		if e != nil {
			return e
		}
		return atomicWrite(manifest, b)
	}
	if err = save(); err != nil {
		return p, err
	}
	for _, rel := range p.Paths {
		src := filepath.Join(s.root, filepath.FromSlash(rel))
		dst := filepath.Join(backup, "files", filepath.FromSlash(rel))
		if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return p, err
		}
		if err = copyMigrationTree(src, dst); err != nil {
			return p, err
		}
	}
	p.Status = "backup_complete"
	if err = save(); err != nil {
		return p, err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return p, err
	}
	if err = os.Mkdir(target, 0700); err != nil {
		return p, err
	}
	for _, rel := range p.Paths {
		dst := filepath.Join(target, filepath.FromSlash(rel))
		if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return p, err
		}
		if err = copyMigrationTree(filepath.Join(backup, "files", filepath.FromSlash(rel)), dst); err != nil {
			return p, err
		}
	}
	for _, filename := range []string{"recent-project.json", "asset-library.json"} {
		state := filepath.Join(target, "projects", filename)
		b, e := os.ReadFile(state)
		if e == nil {
			var v any
			if err = json.Unmarshal(b, &v); err != nil {
				return p, err
			}
			b, err = json.MarshalIndent(migrationRewrite(v, s.root, target), "", "  ")
			if err != nil {
				return p, err
			}
			if err = atomicWrite(state, b); err != nil {
				return p, err
			}
		} else if !os.IsNotExist(e) {
			return p, e
		}
	}
	p.Status = "files_copied"
	if err = save(); err != nil {
		return p, err
	}
	if s.pool != nil {
		tag, e := s.pool.Exec(ctx, "UPDATE projects SET root=$2,legacy=false WHERE id=$1 AND root='.' AND owner_id=$3", id, p.NewRoot, p.OwnerID)
		if e != nil {
			return p, e
		}
		if tag.RowsAffected() != 1 {
			return p, ErrConflict
		}
	} else {
		err = s.mutate(func(d *snapshot) error {
			project, ok := d.Projects[id]
			if !ok || project.Root != "." || project.OwnerID != p.OwnerID {
				return ErrConflict
			}
			project.Root = p.NewRoot
			project.Legacy = false
			d.Projects[id] = project
			return nil
		})
		if err != nil {
			return p, err
		}
	}
	p.Status = "committed"
	if err = save(); err != nil {
		return p, fmt.Errorf("metadata committed; final manifest update: %w", err)
	}
	return p, nil
}
func (s *Store) RollbackLegacyMigration(ctx context.Context, manifest string) error {
	b, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	var p MigrationPlan
	if err = json.Unmarshal(b, &p); err != nil {
		return err
	}
	if p.ProjectID == "" || p.OldRoot != "." || p.NewRoot != "projects/"+p.ProjectID {
		return ErrInvalid
	}
	if s.pool != nil {
		tag, err := s.pool.Exec(ctx, "UPDATE projects SET root='.',legacy=true WHERE id=$1 AND owner_id=$2 AND root=$3", p.ProjectID, p.OwnerID, p.NewRoot)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return nil
	}
	return s.mutate(func(d *snapshot) error {
		v, ok := d.Projects[p.ProjectID]
		if !ok || v.OwnerID != p.OwnerID || v.Root != p.NewRoot {
			return ErrConflict
		}
		v.Root = "."
		v.Legacy = true
		d.Projects[v.ID] = v
		return nil
	})
}
