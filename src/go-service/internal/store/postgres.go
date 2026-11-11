package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"time"
)

const schema = `CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY,username TEXT UNIQUE NOT NULL,display_name TEXT DEFAULT '',salt TEXT NOT NULL,hash TEXT NOT NULL,is_admin BOOLEAN NOT NULL DEFAULT false,daily_page_quota INTEGER NOT NULL DEFAULT 0,created_at TIMESTAMPTZ DEFAULT now(),last_login_at TIMESTAMPTZ,usage_pages BIGINT NOT NULL DEFAULT 0,usage_images BIGINT NOT NULL DEFAULT 0,usage_storyboards BIGINT NOT NULL DEFAULT 0,usage_characters BIGINT NOT NULL DEFAULT 0,default_image_model_id TEXT NOT NULL DEFAULT '',default_text_model_id TEXT NOT NULL DEFAULT '');
ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS salt TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS daily_page_quota INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT now();
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS usage_pages BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS usage_images BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS usage_storyboards BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS usage_characters BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS default_image_model_id TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS default_text_model_id TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS credits NUMERIC NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS credit_status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE users ADD COLUMN IF NOT EXISTS banned_reason TEXT DEFAULT '';
CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY,user_id TEXT NOT NULL,expires_at BIGINT NOT NULL,created_at TIMESTAMPTZ DEFAULT now());
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT now();
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_exp ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY,owner_id TEXT NOT NULL DEFAULT '',name TEXT NOT NULL DEFAULT '',root TEXT NOT NULL DEFAULT '.',legacy BOOLEAN NOT NULL DEFAULT false,created_at TIMESTAMPTZ DEFAULT now(),updated_at TIMESTAMPTZ);
ALTER TABLE projects ADD COLUMN IF NOT EXISTS owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN IF NOT EXISTS root TEXT NOT NULL DEFAULT '.';
ALTER TABLE projects ADD COLUMN IF NOT EXISTS legacy BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT now();
ALTER TABLE projects ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_projects_owner ON projects(owner_id);
CREATE TABLE IF NOT EXISTS user_active(user_id TEXT PRIMARY KEY,project_id TEXT NOT NULL);`

func (s *Store) initPG(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, schema)
	return err
}
func pgError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return ErrConflict
	}
	return err
}
func (s *Store) pgUser(ctx context.Context, col, v string) (User, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, "SELECT to_jsonb(u) FROM users u WHERE "+col+"=$1", v).Scan(&raw)
	if err != nil {
		return User{}, pgError(err)
	}
	var u User
	err = json.Unmarshal(raw, &u)
	return u, err
}
func (s *Store) pgCreateUser(ctx context.Context, u User) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO users(id,username,display_name,salt,hash,is_admin,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, u.ID, u.Username, u.DisplayName, u.Salt, u.Hash, u.IsAdmin, u.CreatedAt)
	return pgError(err)
}
func (s *Store) pgChangePassword(ctx context.Context, id, salt, hash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "UPDATE users SET salt=$2,hash=$3 WHERE id=$1", id, salt, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id=$1", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const projectCols = "id,owner_id,name,root,legacy,created_at,updated_at"

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Root, &p.Legacy, &p.CreatedAt, &p.UpdatedAt)
	return p, pgError(err)
}
func (s *Store) pgProject(ctx context.Context, owner, id string) (Project, error) {
	return scanProject(s.pool.QueryRow(ctx, "SELECT "+projectCols+" FROM projects WHERE id=$1 AND owner_id=$2", id, owner))
}
func (s *Store) pgListProjects(ctx context.Context, owner string) ([]Project, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+projectCols+" FROM projects WHERE owner_id=$1 ORDER BY created_at,id", owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) pgInsertProject(ctx context.Context, p Project) error {
	_, err := s.pool.Exec(ctx, "INSERT INTO projects(id,owner_id,name,root,legacy,created_at) VALUES($1,$2,$3,$4,$5,$6)", p.ID, p.OwnerID, p.Name, p.Root, p.Legacy, p.CreatedAt)
	return pgError(err)
}
func (s *Store) pgRenameProject(ctx context.Context, owner, id, name string) (Project, error) {
	return scanProject(s.pool.QueryRow(ctx, "UPDATE projects SET name=$3,updated_at=$4 WHERE id=$1 AND owner_id=$2 RETURNING "+projectCols, id, owner, strings.TrimSpace(name), time.Now().UTC()))
}
func (s *Store) pgDeleteProject(ctx context.Context, owner, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "DELETE FROM projects WHERE id=$1 AND owner_id=$2", id, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err = tx.Exec(ctx, "DELETE FROM user_active WHERE project_id=$1", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) pgActive(ctx context.Context, owner string) (Project, error) {
	var id string
	err := s.pool.QueryRow(ctx, "SELECT project_id FROM user_active WHERE user_id=$1", owner).Scan(&id)
	if err != nil {
		return Project{}, pgError(err)
	}
	return s.GetProject(ctx, owner, id)
}
func (s *Store) pgSetActive(ctx context.Context, owner, id string) error {
	tag, err := s.pool.Exec(ctx, `INSERT INTO user_active(user_id,project_id) SELECT $1,id FROM projects WHERE id=$2 AND owner_id=$1 ON CONFLICT(user_id) DO UPDATE SET project_id=EXCLUDED.project_id`, owner, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
