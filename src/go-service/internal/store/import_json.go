package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type JSONImportPlan struct {
	Users            int               `json:"users"`
	Sessions         int               `json:"sessions"`
	Projects         int               `json:"projects"`
	OwnerAssignments map[string]string `json:"ownerAssignments"`
	ExpiredSessions  int               `json:"expiredSessions"`
	DryRun           bool              `json:"dryRun"`
	Backup           string            `json:"backup,omitempty"`
}
type legacyUser struct {
	ID, Username, DisplayName, Salt, Hash string
	IsAdmin                               bool
	CreatedAt, LastLoginAt                string
	Usage                                 struct{ Pages, Images, Storyboards, Characters int64 }
}

func legacyTime(v string) time.Time {
	t, e := time.Parse(time.RFC3339Nano, v)
	if e != nil {
		return time.Now().UTC()
	}
	return t
}

// ImportLegacyJSON never overwrites an existing Go snapshot. All originals stay
// intact. Legacy shared-root projects are copied before publishing go-store.json.
func ImportLegacyJSON(ctx context.Context, dataRoot string, apply bool) (JSONImportPlan, error) {
	plan := JSONImportPlan{DryRun: !apply, OwnerAssignments: map[string]string{}}
	root, e := filepath.Abs(dataRoot)
	if e != nil {
		return plan, e
	}
	target := filepath.Join(root, "go-store.json")
	if _, e = os.Lstat(target); !os.IsNotExist(e) {
		return plan, fmt.Errorf("go-store.json already exists; choose an unused copy of the data root")
	}
	read := func(rel string, v any, optional bool) error {
		b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if os.IsNotExist(e) && optional {
			return nil
		}
		if e != nil {
			return e
		}
		return json.Unmarshal(b, v)
	}
	var accounts struct{ Users []legacyUser }
	if e = read("accounts.json", &accounts, false); e != nil {
		return plan, e
	}
	if len(accounts.Users) == 0 {
		return plan, fmt.Errorf("accounts.json contains no users")
	}
	d := snapshot{map[string]User{}, map[string]Session{}, map[string]Project{}, map[string]string{}}
	names := map[string]bool{}
	for i, v := range accounts.Users {
		name := strings.ToLower(strings.TrimSpace(v.Username))
		salt, se := hex.DecodeString(v.Salt)
		hash, he := hex.DecodeString(v.Hash)
		if v.ID == "" || name == "" || names[name] || d.Users[v.ID].ID != "" || se != nil || he != nil || len(salt) != 16 || len(hash) != 32 {
			return plan, fmt.Errorf("invalid or duplicate account at index %d", i)
		}
		names[name] = true
		u := User{ID: v.ID, Username: name, DisplayName: v.DisplayName, Salt: v.Salt, Hash: v.Hash, IsAdmin: v.IsAdmin || i == 0, CreatedAt: legacyTime(v.CreatedAt), CreditStatus: "active", UsagePages: v.Usage.Pages, UsageImages: v.Usage.Images, UsageStoryboards: v.Usage.Storyboards, UsageCharacters: v.Usage.Characters}
		if u.DisplayName == "" {
			u.DisplayName = v.Username
		}
		if v.LastLoginAt != "" {
			t := legacyTime(v.LastLoginAt)
			u.LastLoginAt = &t
		}
		d.Users[u.ID] = u
	}
	var sessions struct {
		Sessions map[string]struct {
			UserID    string `json:"userId"`
			ExpiresAt int64  `json:"expiresAt"`
		}
	}
	if e = read("sessions.json", &sessions, true); e != nil {
		return plan, e
	}
	for token, v := range sessions.Sessions {
		if v.ExpiresAt <= time.Now().UnixMilli() {
			plan.ExpiredSessions++
			continue
		}
		if token == "" || d.Users[v.UserID].ID == "" {
			return plan, fmt.Errorf("session references an unknown user")
		}
		d.Sessions[token] = Session{Token: token, UserID: v.UserID, ExpiresAt: v.ExpiresAt}
	}
	var index struct {
		Items []struct {
			ID, OwnerID, Name, Root string
			Legacy                  bool
			CreatedAt, UpdatedAt    string
		}
		ActiveBy map[string]string
		ActiveID string
	}
	if e = read("projects/index.json", &index, true); e != nil {
		return plan, e
	}
	first := accounts.Users[0].ID
	for _, v := range index.Items {
		if v.ID == "" || strings.ContainsAny(v.ID, "/\\:") || v.ID == "." || v.ID == ".." || d.Projects[v.ID].ID != "" {
			return plan, ErrInvalid
		}
		owner := v.OwnerID
		if owner == "" {
			owner = first
		}
		if d.Users[owner].ID == "" {
			return plan, fmt.Errorf("project %s references unknown owner", v.ID)
		}
		rel := strings.ReplaceAll(v.Root, "\\", "/")
		if rel == "" {
			rel = "."
		}
		if rel != "." && rel != "projects/"+v.ID {
			return plan, fmt.Errorf("project %s has unsupported root %s", v.ID, rel)
		}
		p := Project{ID: v.ID, OwnerID: owner, Name: v.Name, Root: rel, Legacy: v.Legacy, CreatedAt: legacyTime(v.CreatedAt)}
		if v.UpdatedAt != "" {
			t := legacyTime(v.UpdatedAt)
			p.UpdatedAt = &t
		}
		d.Projects[p.ID] = p
		plan.OwnerAssignments[p.ID] = owner
	}
	for user, pid := range index.ActiveBy {
		if d.Users[user].ID == "" || d.Projects[pid].OwnerID != user {
			return plan, fmt.Errorf("invalid active project mapping")
		}
		d.Active[user] = pid
	}
	if index.ActiveID != "" && d.Active[first] == "" {
		if d.Projects[index.ActiveID].OwnerID != first {
			return plan, fmt.Errorf("invalid legacy activeId")
		}
		d.Active[first] = index.ActiveID
	}
	plan.Users = len(d.Users)
	plan.Sessions = len(d.Sessions)
	plan.Projects = len(d.Projects)
	if !apply {
		return plan, nil
	}
	backup := filepath.Join(root, "migration-backups", time.Now().UTC().Format("20060102T150405.000000000")+"-json")
	if e = os.MkdirAll(backup, 0700); e != nil {
		return plan, e
	}
	plan.Backup = backup
	for _, rel := range []string{"accounts.json", "sessions.json", "projects/index.json"} {
		src := filepath.Join(root, filepath.FromSlash(rel))
		if _, e = os.Lstat(src); os.IsNotExist(e) {
			continue
		}
		dst := filepath.Join(backup, filepath.FromSlash(rel))
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return plan, e
		}
		if e = copyMigrationTree(src, dst); e != nil {
			return plan, e
		}
	}
	b, _ := json.MarshalIndent(plan, "", "  ")
	if e = atomicWrite(filepath.Join(backup, "manifest.json"), b); e != nil {
		return plan, e
	}
	s := &Store{root: root, file: filepath.Join(backup, "staged-go-store.json"), data: d}
	for id, p := range d.Projects {
		if p.Root == "." {
			if _, e = s.ApplyLegacyMigration(ctx, id); e != nil {
				return plan, e
			}
		}
	}
	b, e = json.Marshal(s.data)
	if e != nil {
		return plan, e
	} // Exclusive publication prevents concurrent import overwrite.
	f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return plan, e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return plan, e
	}
	return plan, ce
}
