package httpapi

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"comic.local/go-service/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrLimit is deliberately free of database, provider and account details.
var ErrLimit = errors.New("生成请求已达到限额，请稍后重试")
var errLimitAccounting = errors.New("生成额度服务暂不可用")

type generationLimitEntry struct {
	attempts []time.Time
	active   int
}
type generationLimitState struct {
	mu    sync.Mutex
	users map[string]*generationLimitEntry
}

// reserve counts accepted attempts even when generation later fails. The limits
// apply across all projects and generation kinds for one user in this process.
func (l *generationLimitState) reserve(uid string, now time.Time) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.users == nil {
		l.users = map[string]*generationLimitEntry{}
	}
	// Bound memory by dropping inactive accounts whose rolling window expired.
	for id, e := range l.users {
		if e.active == 0 && (len(e.attempts) == 0 || !e.attempts[len(e.attempts)-1].After(now.Add(-time.Minute))) {
			delete(l.users, id)
		}
	}
	e := l.users[uid]
	if e == nil {
		e = &generationLimitEntry{}
		l.users[uid] = e
	}
	first := 0
	for first < len(e.attempts) && !e.attempts[first].After(now.Add(-time.Minute)) {
		first++
	}
	e.attempts = e.attempts[first:]
	if e.active >= 2 || len(e.attempts) >= 10 {
		return nil, ErrLimit
	}
	e.attempts = append(e.attempts, now)
	e.active++
	var once sync.Once
	return func() { once.Do(func() { l.mu.Lock(); defer l.mu.Unlock(); e.active-- }) }, nil
}
func defaultDailyPageQuota() int {
	if n, e := strconv.Atoi(os.Getenv("AI_COMIC_DAILY_PAGE_QUOTA")); e == nil && n > 0 {
		return n
	}
	return 80
}

const generationUsageSchema = `CREATE TABLE IF NOT EXISTS usage_daily (
 user_id TEXT NOT NULL, day DATE NOT NULL, pages INTEGER NOT NULL DEFAULT 0,
 images INTEGER NOT NULL DEFAULT 0, storyboards INTEGER NOT NULL DEFAULT 0,
 text_calls INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(user_id,day));`

// checkGenerationLimit must run before any upstream call. Always invoke release
// exactly once with the final generation result (defer is recommended).
// Admins bypass limits; ordinary JSON-mode users cannot generate.
func (s *Server) checkGenerationLimit(ctx context.Context, u store.PublicUser, pid, kind string) (func(bool), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if u.IsAdmin {
		return func(bool) {}, nil
	}
	if u.ID == "" || pid == "" || s.Store == nil || s.Store.Pool() == nil {
		return nil, store.ErrForbidden
	}
	free, err := s.generationLimits.reserve(u.ID, time.Now())
	if err != nil {
		return nil, err
	}
	pool := s.Store.Pool()
	day := time.Now().Format("2006-01-02")
	if err = reserveGenerationUsage(ctx, pool, u.ID, day, kind, defaultDailyPageQuota()); err != nil {
		free()
		if errors.Is(err, ErrLimit) || errors.Is(err, store.ErrForbidden) {
			return nil, err
		}
		if s.Logger != nil {
			s.Logger.Error("generation quota reservation failed", "error", err)
		}
		return nil, errLimitAccounting
	}
	var once sync.Once
	return func(success bool) {
		once.Do(func() {
			defer free()
			// A canceled client still needs durable quota compensation/accounting.
			finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if e := finishGenerationUsage(finishCtx, pool, u.ID, day, kind, success); e != nil && s.Logger != nil {
				s.Logger.Error("generation quota completion failed", "error", e)
			}
		})
	}, nil
}
func reserveGenerationUsage(ctx context.Context, pool *pgxpool.Pool, uid, day, kind string, fallback int) error {
	if _, err := pool.Exec(ctx, generationUsageSchema); err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var quota int
	var admin bool
	var status string
	// Lock the account so quota edits and concurrent processes cannot race checks.
	err = tx.QueryRow(ctx, "SELECT daily_page_quota,is_admin,credit_status FROM users WHERE id=$1 FOR UPDATE", uid).Scan(&quota, &admin, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrForbidden
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return store.ErrForbidden
	}
	if quota <= 0 {
		quota = fallback
	}
	if _, err = tx.Exec(ctx, "INSERT INTO usage_daily(user_id,day) VALUES($1,$2::date) ON CONFLICT DO NOTHING", uid, day); err != nil {
		return err
	}
	if kind == "page" {
		var pages int
		if err = tx.QueryRow(ctx, "SELECT pages FROM usage_daily WHERE user_id=$1 AND day=$2::date FOR UPDATE", uid, day).Scan(&pages); err != nil {
			return err
		}
		if !admin && pages >= quota {
			return ErrLimit
		}
		if _, err = tx.Exec(ctx, "UPDATE usage_daily SET pages=pages+1 WHERE user_id=$1 AND day=$2::date", uid, day); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func finishGenerationUsage(ctx context.Context, pool *pgxpool.Pool, uid, day, kind string, success bool) error {
	// Page reservations are already counted. Failed work returns only its own slot.
	if !success {
		if kind == "page" {
			_, err := pool.Exec(ctx, "UPDATE usage_daily SET pages=GREATEST(pages-1,0) WHERE user_id=$1 AND day=$2::date", uid, day)
			return err
		}
		return nil
	}
	column := "text_calls"
	switch kind {
	case "page", "character", "scene":
		column = "images"
	case "storyboard":
		column = "storyboards"
	}
	_, err := pool.Exec(ctx, "UPDATE usage_daily SET "+column+"="+column+"+1 WHERE user_id=$1 AND day=$2::date", uid, day)
	return err
}
