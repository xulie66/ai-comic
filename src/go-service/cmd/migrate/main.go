// migrate is an offline, copy-first legacy-project migration tool.
package main

import (
	"comic.local/go-service/internal/config"
	"comic.local/go-service/internal/store"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	apply := flag.Bool("apply", false, "apply migration; stop every service process first")
	dry := flag.Bool("dry-run", true, "list plan only; -apply explicitly enables writes")
	id := flag.String("project", "", "project ID to migrate (required for apply)")
	rollback := flag.String("rollback-manifest", "", "rollback metadata using this manifest; keeps all copied files")
	migrateJSON := flag.Bool("migrate-json", false, "import legacy accounts/sessions/project index to a new Go snapshot")
	flag.Parse()
	_ = dry
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if *migrateJSON {
		if c.DatabaseURL != "" {
			return fmt.Errorf("-migrate-json requires an empty database URL; local JSON target only")
		}
		plan, e := store.ImportLegacyJSON(ctx, c.DataRoot, *apply)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(plan)
		return e
	}
	if c.DatabaseURL == "" {
		for _, rel := range []string{"accounts.json", "sessions.json", "projects/index.json"} {
			file := filepath.Join(c.DataRoot, filepath.FromSlash(rel))
			if st, e := os.Stat(file); e == nil {
				fmt.Printf("Legacy JSON source: %s (%d bytes)\n", file, st.Size())
			}
		}
		if _, err = os.Stat(filepath.Join(c.DataRoot, "go-store.json")); os.IsNotExist(err) {
			return fmt.Errorf("no Go snapshot exists; use -migrate-json for a validated dry-run or -migrate-json -apply to import")
		}
	}
	s, err := store.Open(ctx, store.Options{DataRoot: c.DataRoot, DatabaseURL: c.DatabaseURL, SkipSchema: true})
	if err != nil {
		return err
	}
	defer s.Close()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if *rollback != "" {
		if !*apply {
			return fmt.Errorf("rollback requires -apply and all service processes stopped")
		}
		if err = s.RollbackLegacyMigration(ctx, *rollback); err != nil {
			return err
		}
		return enc.Encode(map[string]any{"status": "metadata_rolled_back", "manifest": *rollback, "filesRetained": true})
	}
	plans, err := s.LegacyMigrationPlans(ctx)
	if err != nil {
		return err
	}
	if !*apply {
		return enc.Encode(map[string]any{"dryRun": true, "dataRoot": s.DataRoot(), "projects": plans, "note": "Stop all services before -apply. Originals remain unchanged; backups and manifest are retained."})
	}
	if *id == "" {
		return fmt.Errorf("-apply requires explicit -project ID")
	}
	p, err := s.ApplyLegacyMigration(ctx, *id)
	_ = enc.Encode(p)
	return err
}
