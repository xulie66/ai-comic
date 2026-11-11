package httpapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"comic.local/go-service/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGenerationLimitConcurrencyAndIdempotentRelease(t *testing.T) {
	var state generationLimitState
	now := time.Now()
	first, e := state.reserve("u", now)
	if e != nil {
		t.Fatal(e)
	}
	second, e := state.reserve("u", now)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = state.reserve("u", now); !errors.Is(e, ErrLimit) {
		t.Fatalf("third concurrent request: %v", e)
	}
	first()
	first()
	third, e := state.reserve("u", now)
	if e != nil {
		t.Fatal(e)
	}
	third()
	second()
	// Each user owns an independent window and active count.
	other, e := state.reserve("other", now)
	if e != nil {
		t.Fatal(e)
	}
	other()
}
func TestGenerationLimitRollingMinuteCountsFailures(t *testing.T) {
	var state generationLimitState
	now := time.Now()
	for i := 0; i < 10; i++ {
		free, e := state.reserve("u", now.Add(time.Duration(i)*time.Millisecond))
		if e != nil {
			t.Fatal(e)
		}
		free()
	}
	if _, e := state.reserve("u", now.Add(59*time.Second)); !errors.Is(e, ErrLimit) {
		t.Fatalf("eleventh request: %v", e)
	}
	free, e := state.reserve("u", now.Add(time.Minute))
	if e != nil {
		t.Fatalf("expired first attempt still blocks: %v", e)
	}
	free()
}
func TestGenerationLimitConcurrentReservations(t *testing.T) {
	var state generationLimitState
	var accepted atomic.Int32
	var wg sync.WaitGroup
	gate := make(chan struct{})
	checked := make(chan struct{}, 40)
	now := time.Now()
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			free, e := state.reserve("u", now)
			if e == nil {
				accepted.Add(1)
				checked <- struct{}{}
				<-gate
				free()
			} else {
				checked <- struct{}{}
				if !errors.Is(e, ErrLimit) {
					t.Error(e)
				}
			}
		}()
	}
	for i := 0; i < 40; i++ {
		select {
		case <-checked:
		case <-time.After(time.Second):
			t.Fatal("reservations stalled")
		}
	}
	close(gate)
	wg.Wait()
	if n := accepted.Load(); n != 2 {
		t.Fatalf("accepted %d concurrent calls want 2", n)
	}
}
func TestGenerationLimitAdminJSONAndCanceledContext(t *testing.T) {
	s := &Server{}
	release, e := s.checkGenerationLimit(context.Background(), store.PublicUser{ID: "admin", IsAdmin: true}, "project", "page")
	if e != nil {
		t.Fatal(e)
	}
	release(true)
	release(false)
	if _, e = s.checkGenerationLimit(context.Background(), store.PublicUser{ID: "u"}, "project", "page"); !errors.Is(e, store.ErrForbidden) {
		t.Fatalf("ordinary JSON user: %v", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = s.checkGenerationLimit(ctx, store.PublicUser{IsAdmin: true}, "p", "page"); !errors.Is(e, context.Canceled) {
		t.Fatalf("canceled context: %v", e)
	}
	t.Setenv("AI_COMIC_DAILY_PAGE_QUOTA", "")
	if defaultDailyPageQuota() != 80 {
		t.Fatal("Node default must be 80")
	}
	t.Setenv("AI_COMIC_DAILY_PAGE_QUOTA", "12")
	if defaultDailyPageQuota() != 12 {
		t.Fatal("configured default ignored")
	}
}

// Uses a disposable, isolated schema; does not call any external model provider.
func TestGenerationQuotaPostgresTransactions(t *testing.T) {
	dsn := os.Getenv("OPERATIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set OPERATIONS_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("quota_test_%d", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	_, e = pool.Exec(ctx, `CREATE TABLE users(id text PRIMARY KEY,daily_page_quota int NOT NULL DEFAULT 0,is_admin bool NOT NULL DEFAULT false,credit_status text NOT NULL DEFAULT 'active');INSERT INTO users(id,daily_page_quota) VALUES('u',1),('default',0);`)
	if e != nil {
		t.Fatal(e)
	}
	day := time.Now().Format("2006-01-02")
	// Initialize once; concurrent reservations then compete on real row locks.
	if _, e = pool.Exec(ctx, generationUsageSchema); e != nil {
		t.Fatal(e)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := reserveGenerationUsage(ctx, pool, "u", day, "page", 80)
			if e == nil {
				wins.Add(1)
			} else if !errors.Is(e, ErrLimit) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("quota overbooked: %d", wins.Load())
	}
	if e = finishGenerationUsage(ctx, pool, "u", day, "page", false); e != nil {
		t.Fatal(e)
	}
	if e = reserveGenerationUsage(ctx, pool, "u", day, "page", 80); e != nil {
		t.Fatalf("failed page did not return slot: %v", e)
	}
	if e = finishGenerationUsage(ctx, pool, "u", day, "page", true); e != nil {
		t.Fatal(e)
	}
	var pages, images int
	if e = pool.QueryRow(ctx, "SELECT pages,images FROM usage_daily WHERE user_id='u' AND day=$1::date", day).Scan(&pages, &images); e != nil {
		t.Fatal(e)
	}
	if pages != 1 || images != 1 {
		t.Fatalf("usage pages=%d images=%d", pages, images)
	}
	if e = reserveGenerationUsage(ctx, pool, "default", day, "page", 1); e != nil {
		t.Fatal(e)
	}
	if e = reserveGenerationUsage(ctx, pool, "default", day, "page", 1); !errors.Is(e, ErrLimit) {
		t.Fatalf("quota zero did not use global fallback: %v", e)
	}
	if e = reserveGenerationUsage(ctx, pool, "u", day, "storyboard", 80); e != nil {
		t.Fatalf("text should not reserve page quota: %v", e)
	}
	if e = finishGenerationUsage(ctx, pool, "u", day, "storyboard", true); e != nil {
		t.Fatal(e)
	}
	var storyboards int
	if e = pool.QueryRow(ctx, "SELECT storyboards FROM usage_daily WHERE user_id='u' AND day=$1::date", day).Scan(&storyboards); e != nil || storyboards != 1 {
		t.Fatalf("storyboard usage %d: %v", storyboards, e)
	}
}
