package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

// Runs only against an explicitly supplied database, in a unique private schema.
func TestPostgresUpgradeLegacyUsers(t *testing.T) {
	url := os.Getenv("STORE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set STORE_TEST_DATABASE_URL for PostgreSQL upgrade integration fixture")
	}
	ctx := context.Background()
	admin, e := pgx.Connect(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close(ctx)
	name := NewID("store_upgrade")
	ident := pgx.Identifier{name}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+ident); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, e := admin.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	cfg, e := pgxpool.ParseConfig(url)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if _, e = pool.Exec(ctx, `CREATE TABLE users(id TEXT PRIMARY KEY,username TEXT UNIQUE NOT NULL,salt TEXT NOT NULL,hash TEXT NOT NULL); INSERT INTO users(id,username,salt,hash) VALUES('legacy','alice','old-salt','old-hash'); CREATE TABLE projects(id TEXT PRIMARY KEY);`); e != nil {
		t.Fatal(e)
	}
	s := &Store{pool: pool}
	for i := 0; i < 2; i++ {
		if e = s.initPG(ctx); e != nil {
			t.Fatalf("upgrade pass %d: %v", i, e)
		}
	}
	u, e := s.UserByID(ctx, "legacy")
	if e != nil {
		t.Fatal(e)
	}
	if u.Salt != "old-salt" || u.Hash != "old-hash" || u.Username != "alice" {
		t.Fatal("upgrade modified credentials", u)
	}
	for _, col := range []string{"display_name", "salt", "hash", "is_admin", "daily_page_quota", "created_at", "last_login_at", "usage_pages", "usage_images", "usage_storyboards", "usage_characters", "default_image_model_id", "default_text_model_id", "credits", "credit_status", "banned_reason"} {
		var count int
		if e = pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name='users' AND column_name=$2", name, col).Scan(&count); e != nil || count != 1 {
			t.Fatalf("missing column %s: %v", col, e)
		}
	}
	if _, e = pool.Exec(ctx, "UPDATE users SET default_image_model_id='image',default_text_model_id='text' WHERE id='legacy'"); e != nil {
		t.Fatal(e)
	}
	u, e = s.UserByID(ctx, "legacy")
	if e != nil || u.DefaultImageModelID != "image" || u.DefaultTextModelID != "text" {
		t.Fatal("upgraded fields unusable", u, e)
	}
}
