package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"comic.local/go-service/internal/auth"
	"comic.local/go-service/internal/generation"
	"comic.local/go-service/internal/operations"
	"comic.local/go-service/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

type billingHTTPFixture struct {
	api       *regressionAPI
	uid, pid  string
	calls     atomic.Int32
	withUsage bool
	image     []byte
}

func newBillingHTTPFixture(t *testing.T, withUsage bool) *billingHTTPFixture {
	t.Helper()
	dsn := os.Getenv("OPERATIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set OPERATIONS_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	ctx := context.Background()
	root, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := store.NewID("http_billing")
	if _, e = root.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		root.Close()
		t.Fatal(e)
	}
	t.Cleanup(func() { root.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); root.Close() })
	// Pass search_path through libpq connection options so Store and Ops share the exact schema.
	if u, e := url.Parse(dsn); e == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " search_path=" + schema
	}
	st, e := store.Open(ctx, store.Options{DataRoot: t.TempDir(), DatabaseURL: dsn})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ops := operations.New(st.Pool(), logger)
	ops.DataRoot = st.DataRoot()
	if e = ops.Init(ctx); e != nil {
		t.Fatal(e)
	}
	a := auth.New(st, auth.Options{CookieName: "billing_fixture"})
	registered, e := a.Register(ctx, auth.Credentials{Username: "billing_user", Password: "local-fixture-password"})
	if e != nil {
		t.Fatal(e)
	}
	project, e := st.CreateProject(ctx, registered.User.ID, "计费回归")
	if e != nil {
		t.Fatal(e)
	}
	if e = st.SetActiveProject(ctx, registered.User.ID, project.ID); e != nil {
		t.Fatal(e)
	}
	f := &billingHTTPFixture{uid: registered.User.ID, pid: project.ID, withUsage: withUsage, image: []byte("\x89PNG\r\n\x1a\nbilling-local-image")}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("unexpected fake path %s", r.URL.Path)
		}
		packet := map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(f.image)}}}
		if f.withUsage {
			packet["usage"] = map[string]any{"prompt_tokens": 12, "completion_tokens": 0}
		}
		json.NewEncoder(w).Encode(packet)
	}))
	t.Cleanup(provider.Close)
	api := &regressionAPI{t: t, server: &Server{Store: st, Auth: a, Ops: ops, Client: &generation.Client{HTTP: provider.Client(), Logger: logger}, Logger: logger}}
	login := api.request("POST", "/api/auth/login", map[string]any{"username": "billing_user", "password": "local-fixture-password"}, false)
	api.status(login, 200)
	cookies := login.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login cookie missing")
	}
	api.cookie = cookies[0]
	f.api = api
	_, e = st.Pool().Exec(ctx, `INSERT INTO settings(key,value) VALUES('credits','{"enabled":true,"chargeAdmin":true}'); INSERT INTO channel_models(id,channel_id,model_key,capability,billing_mode,unit_price_micro,input_token_price_micro) VALUES('cm','c','fixture','image','token',2000000,1000000000000);`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = st.Pool().Exec(ctx, `INSERT INTO channels(id,name,base_url,api_key) VALUES('c','local fixture',$1,'fixture')`, provider.URL+"/v1"); e != nil {
		t.Fatal(e)
	}
	if _, e = st.Pool().Exec(ctx, `UPDATE users SET default_image_model_id='cm',daily_page_quota=1 WHERE id=$1`, f.uid); e != nil {
		t.Fatal(e)
	}
	api.save(map[string]any{"storyboard": map[string]any{"pages": []any{map[string]any{"page_index": 1, "panels": []any{map[string]any{"visual_prompt": "本地回归"}}}}}})
	return f
}
func (f *billingHTTPFixture) user(t *testing.T) store.PublicUser {
	t.Helper()
	u, e := f.api.server.Store.UserByID(context.Background(), f.uid)
	if e != nil {
		t.Fatal(e)
	}
	return u.Public()
}
func (f *billingHTTPFixture) fund(t *testing.T, n int64) {
	t.Helper()
	if _, e := f.api.server.Ops.AdjustCredits(context.Background(), f.uid, n, "setup", store.NewID("fund"), "fixture", "test"); e != nil {
		t.Fatal(e)
	}
}
func (f *billingHTTPFixture) payload(ref string) map[string]any {
	return map[string]any{"page": map[string]any{"page_index": 1, "panels": []any{map[string]any{"visual_prompt": "本地回归"}}}, "_jobTaskRef": ref}
}
func (f *billingHTTPFixture) balance(t *testing.T) int64 {
	t.Helper()
	b, e := f.api.server.Ops.Balance(context.Background(), f.uid)
	if e != nil {
		t.Fatal(e)
	}
	return b.BalanceMicro
}
func (f *billingHTTPFixture) usage(t *testing.T) (int, int) {
	t.Helper()
	var p, i int
	e := f.api.server.Store.Pool().QueryRow(context.Background(), "SELECT pages,images FROM usage_daily WHERE user_id=$1 AND day=$2::date", f.uid, time.Now().Format("2006-01-02")).Scan(&p, &i)
	if e != nil {
		t.Fatal(e)
	}
	return p, i
}

func TestBillingHTTPActualUsageDebtRetainsArtifactAndBlocksNextTask(t *testing.T) {
	f := newBillingHTTPFixture(t, true)
	f.fund(t, 10000000)
	// Exercise the authenticated HTTP entry, including real quota reservation.
	result := f.api.json(f.api.request("POST", "/api/generate/page", f.payload("first"), true))
	path := str(result, "imagePath")
	if path == "" {
		t.Fatal("generated artifact missing")
	}
	if balance := f.balance(t); balance != -2000000 {
		t.Fatalf("balance=%d want -2000000", balance)
	}
	b, e := f.api.server.Store.ReadImage(context.Background(), f.uid, f.pid, path)
	if e != nil || !bytes.Equal(b, f.image) {
		t.Fatalf("artifact lost after settlement: %v", e)
	}
	p, i := f.usage(t)
	if p != 1 || i != 1 {
		t.Fatalf("usage pages=%d images=%d", p, i)
	}
	_, e = f.api.server.generate(context.Background(), f.user(t), f.pid, "page", f.payload("second"))
	if e == nil {
		t.Fatal("debt allowed a new task")
	}
	if f.calls.Load() != 1 {
		t.Fatalf("debt dispatched upstream: %d", f.calls.Load())
	}
	var refunds int
	e = f.api.server.Store.Pool().QueryRow(context.Background(), "SELECT count(*) FROM credit_ledger WHERE user_id=$1 AND kind IN ('refund','generation_refund')", f.uid).Scan(&refunds)
	if e != nil || refunds != 0 {
		t.Fatalf("successful output refunded: %d %v", refunds, e)
	}
}
func TestBillingHTTPMissingUsageChargesReservedUnitAndQuota429(t *testing.T) {
	f := newBillingHTTPFixture(t, false)
	f.fund(t, 10000000)
	f.api.status(f.api.request("POST", "/api/generate/page", f.payload("no-usage"), true), 200)
	if n := f.balance(t); n != 8000000 {
		t.Fatalf("missing usage charged %d want balance 8000000", n)
	}
	// A second ordinary page is denied by quota before any provider call; its precharge is compensated.
	f.api.status(f.api.request("POST", "/api/generate/page", f.payload("quota-block"), true), http.StatusTooManyRequests)
	if f.calls.Load() != 1 || f.balance(t) != 8000000 {
		t.Fatal("quota denial called upstream or kept precharge")
	}
	p, i := f.usage(t)
	if p != 1 || i != 1 {
		t.Fatalf("quota denial altered usage %d/%d", p, i)
	}
}
func TestBillingHTTPReadyReceiptRecoveryDoesNotCallProvider(t *testing.T) {
	f := newBillingHTTPFixture(t, true)
	f.fund(t, 10000000)
	ctx := context.Background()
	ops := f.api.server.Ops
	if _, e := ops.ReserveGeneration(ctx, f.uid, "recover-ready", 2000000); e != nil {
		t.Fatal(e)
	}
	path, e := f.api.server.Store.WriteImage(ctx, f.uid, f.pid, "output/recovered.png", f.image)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.api.server.persistImage(ctx, f.uid, f.pid, "page", f.payload("recover-ready"), path); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"success": true, "taskId": "recover-ready", "imagePath": path})
	if _, e = ops.StageGeneration(ctx, f.uid, "recover-ready", 12000000, true, raw); e != nil {
		t.Fatal(e)
	}
	result, e := f.api.server.generate(ctx, f.user(t), f.pid, "page", f.payload("recover-ready"))
	if e != nil || str(result, "imagePath") != path {
		t.Fatalf("ready recovery %v %v", result, e)
	}
	if f.calls.Load() != 0 || f.balance(t) != -2000000 {
		t.Fatal("ready receipt regenerated or failed to settle debt")
	}
	// A settled replay is also idempotent, despite the now-negative balance.
	result, e = f.api.server.generate(ctx, f.user(t), f.pid, "page", f.payload("recover-ready"))
	if e != nil || str(result, "imagePath") != path || f.calls.Load() != 0 || f.balance(t) != -2000000 {
		t.Fatalf("settled replay: %v %v", result, e)
	}
}
func TestBillingHTTPRegistrationDefaultsAndInvite(t *testing.T) {
	f := newBillingHTTPFixture(t, false)
	body := map[string]any{"username": "invited_user", "password": "fixture-password"}
	f.api.status(f.api.request("POST", "/api/auth/register", body, false), 403)
	_, e := f.api.server.Store.Pool().Exec(context.Background(), `INSERT INTO settings(key,value) VALUES('site','{"registerOpen":true,"inviteRequired":true,"inviteCode":"fixture-invite"}') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`)
	if e != nil {
		t.Fatal(e)
	}
	f.api.status(f.api.request("POST", "/api/auth/register", body, false), 403)
	body["inviteCode"] = "wrong"
	f.api.status(f.api.request("POST", "/api/auth/register", body, false), 403)
	body["inviteCode"] = "fixture-invite"
	w := f.api.request("POST", "/api/auth/register", body, false)
	f.api.status(w, 200)
	if len(w.Result().Cookies()) == 0 {
		t.Fatal("successful invited registration missing cookie")
	}
}
func TestBillingHTTPFailedGenerationReturnsQuotaAndPrecharge(t *testing.T) {
	f := newBillingHTTPFixture(t, false)
	f.fund(t, 10000000)
	// Invalid target page fails after the fake provider response, exercising durable compensation.
	payload := f.payload("bad-page")
	payload["page"] = map[string]any{"page_index": 99}
	_, e := f.api.server.generate(context.Background(), f.user(t), f.pid, "page", payload)
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatalf("unexpected failure: %v", e)
	}
	if f.balance(t) != 10000000 {
		t.Fatal("failed generation retained debit")
	}
	p, i := f.usage(t)
	if p != 0 || i != 0 {
		t.Fatalf("failed generation retained quota %d/%d", p, i)
	}
}
