package operations

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

// Explicit opt-in: creates an isolated schema in a disposable PostgreSQL database.
func TestPostgresConcurrentTransactions(t *testing.T) {
	dsn := os.Getenv("OPERATIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set OPERATIONS_TEST_DATABASE_URL to exercise real PostgreSQL row locks and rollback")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name := newID("ops_test")
	if _, err = root.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
		t.Fatal(err)
	}
	defer root.Exec(ctx, "DROP SCHEMA "+name+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `CREATE TABLE users(id TEXT PRIMARY KEY,username TEXT,credits NUMERIC DEFAULT 0); INSERT INTO users VALUES('u','user',10)`); err != nil {
		t.Fatal(err)
	}
	s := New(pool, nil)
	if err = s.Init(ctx); err != nil {
		t.Fatal(err)
	}
	var initial int64
	if err = pool.QueryRow(ctx, "SELECT credits_micro FROM users WHERE id='u'").Scan(&initial); err != nil || initial != 10000000 {
		t.Fatalf("initial legacy migration %d %v", initial, err)
	}
	var defaultFlag bool
	if err = pool.QueryRow(ctx, "INSERT INTO logical_models(id,code,name) VALUES('migration-check','migration-check','migration-check') RETURNING is_default").Scan(&defaultFlag); err != nil || defaultFlag {
		t.Fatalf("logical default column %v %v", defaultFlag, err)
	}
	run := func(f func() error) {
		var wg sync.WaitGroup
		errs := make(chan error, 16)
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); errs <- f() }()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Error(e)
			}
		}
	}
	run(func() error { _, e := s.Charge(ctx, "u", "same-call", 1000000); return e })
	bal, err := s.Balance(ctx, "u")
	if err != nil || bal.BalanceMicro != 9000000 {
		t.Fatalf("debit: %+v %v", bal, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO payments(id,user_id,amount,credits_micro,provider,status,trade_no) VALUES('p','u',9.90,5000000,'epay','pending','AC1'); INSERT INTO redeem_codes(id,code,credits) VALUES('r','CODE',2)`); err != nil {
		t.Fatal(err)
	}
	run(func() error {
		_, e := s.SettleNotify(ctx, Notification{OutTradeNo: "AC1", TradeNo: "TX1", AmountFen: 990})
		return e
	})
	run(func() error { _, e := s.Redeem(ctx, "u", "CODE"); return e })
	bal, err = s.Balance(ctx, "u")
	if err != nil || bal.BalanceMicro != 16000000 {
		t.Fatalf("credits: %+v %v", bal, err)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM credit_ledger").Scan(&count); err != nil || count != 3 {
		t.Fatalf("ledger %d %v", count, err)
	}
	if _, err = s.SettleNotify(ctx, Notification{OutTradeNo: "AC1", TradeNo: "TX1", AmountFen: 1}); err == nil {
		t.Fatal("settled order accepted incorrect amount")
	}
	if _, err = s.Charge(ctx, "u", "overdraft", 17000000); err == nil {
		t.Fatal("overdraft accepted")
	}
	manual, err := s.manualPayment(ctx, map[string]any{"userId": "u", "amount": float64(10), "credits": float64(3), "status": "paid"}, "test")
	if err != nil {
		t.Fatal("manual create", err)
	}
	if err = s.manualSettle(ctx, manual["id"].(string), "refund", "test"); err != nil {
		t.Fatal("manual refund", err)
	}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":1,"payurl":"https://checkout.example.test/order"}`))
	}))
	defer gateway.Close()
	if _, err = s.patchSettings(ctx, map[string]any{"payment": map[string]any{"enabled": true, "provider": "epay", "epay": map[string]any{"gateway": gateway.URL, "pid": "1001", "key": "test-secret", "publicBaseUrl": "https://comic.example.test"}, "packages": []any{map[string]any{"id": "test-package", "amountYuan": float64(9.9), "bonusPercent": float64(0)}}}}); err != nil {
		t.Fatal(err)
	}
	order, err := s.createOrder(ctx, "u", map[string]any{"packageId": "test-package"})
	if err != nil {
		t.Fatal("epay create", err)
	}
	if _, err = s.SettleNotify(ctx, Notification{OutTradeNo: order["outTradeNo"].(string), TradeNo: "TX2", AmountFen: 990}); err != nil {
		t.Fatal("created epay settlement", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE users SET credits_micro=0,credits=100 WHERE id='u'"); err != nil {
		t.Fatal(err)
	}
	if err = s.Init(ctx); err != nil {
		t.Fatal(err)
	}
	bal, err = s.Balance(ctx, "u")
	if err != nil || bal.BalanceMicro != 0 {
		t.Fatalf("zero balance resurrected: %+v %v", bal, err)
	}
}
