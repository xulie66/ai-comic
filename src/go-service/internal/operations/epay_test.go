package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestMD5SignatureCanonical(t *testing.T) {
	p := map[string]string{"pid": "1001", "money": "9.90", "name": "中文 & 漫画", "empty": "", "sign_type": "MD5", "sign": "ignored"}
	if got := Canonical(p); got != "money=9.90&name=中文 & 漫画&pid=1001" {
		t.Fatal(got)
	}
	sig := MD5Sign(p, "secret")
	p["sign"] = "changed"
	p["sign_type"] = "other"
	if MD5Sign(p, "secret") != sig {
		t.Fatal("excluded fields changed signature")
	}
	p["money"] = "9.91"
	if MD5Sign(p, "secret") == sig {
		t.Fatal("amount must be signed")
	}
}
func TestNotifyValidation(t *testing.T) {
	base := map[string]string{"pid": "1001", "money": "9.90", "out_trade_no": "AC1", "trade_no": "TX1", "trade_status": "TRADE_SUCCESS", "sign_type": "MD5"}
	sign := func() { base["sign"] = MD5Sign(base, "secret") }
	sign()
	n, err := VerifyNotify("1001", "secret", base)
	if err != nil || n.AmountFen != 990 {
		t.Fatalf("%+v %v", n, err)
	}
	base["money"] = "0.01"
	if _, err = VerifyNotify("1001", "secret", base); err == nil {
		t.Fatal("tamper accepted")
	}
	sign()
	if _, err = VerifyNotify("different", "secret", base); err == nil {
		t.Fatal("merchant mismatch accepted")
	}
	base["trade_status"] = "WAIT_BUYER_PAY"
	sign()
	if _, err = VerifyNotify("1001", "secret", base); err == nil {
		t.Fatal("unpaid accepted")
	}
}
func TestMoneyPrecision(t *testing.T) {
	for s, want := range map[string]int64{"0": 0, "9.9": 990, "9.90": 990, "0001.01": 101, " 100.00 ": 10000} {
		got, err := ParseYuanToFen(s)
		if err != nil || got != want {
			t.Fatalf("%q %d %v", s, got, err)
		}
	}
	for _, s := range []string{"", "1.001", "-1", "NaN", "1e2", "1.", ".1", "92233720368547758.99", "9999999999999999999999999"} {
		if _, err := ParseYuanToFen(s); err == nil {
			t.Fatal("accepted", s)
		}
	}
}
func TestSettlementIdempotencyDecision(t *testing.T) {
	for _, test := range []struct {
		status, old, new string
		expected, actual int64
		duplicate, bad   bool
	}{{"pending", "", "tx", 100, 100, false, false}, {"paid", "tx", "tx", 100, 100, true, false}, {"paid", "tx", "tx", 100, 1, false, true}, {"paid", "tx", "other", 100, 100, false, true}, {"refunded", "tx", "tx", 100, 100, false, true}, {"cancelled", "", "tx", 100, 100, false, true}} {
		duplicate, err := settlementDecision(test.status, test.old, test.new, test.expected, test.actual)
		if (err != nil) != test.bad || duplicate != test.duplicate {
			t.Fatalf("%+v => %v %v", test, duplicate, err)
		}
	}
}
func TestNilPoolDisablesPaidOperations(t *testing.T) {
	s := New(nil, nil)
	if err := s.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Charge(context.Background(), "u", "ref", 1); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/pay/create", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
func TestQRAndAnonymousReturn(t *testing.T) {
	s := New(nil, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/pay/return/epay?out_trade_no=A%26B", nil)
	s.ServeHTTP(w, r)
	if w.Code != 302 || w.Header().Get("Location") != "/ui/?pay=return&out_trade_no=A%26B" {
		t.Fatalf("%d %s", w.Code, w.Header().Get("Location"))
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/qr.svg?text=https%3A%2F%2Fexample.test", nil)
	r = r.WithContext(WithIdentity(r.Context(), "u", false))
	s.ServeHTTP(w, r)
	if w.Code != 200 || !contains(w.Body.String(), "<svg") || !contains(w.Header().Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
func TestCredentialRedaction(t *testing.T) {
	v := publicSettings(map[string]any{"payment": map[string]any{"epay": map[string]any{"key": "SECRET", "gateway": "https://example.test"}}, "email": map[string]any{"password": "PASSWORD"}})
	raw, _ := json.Marshal(v)
	for _, needle := range []string{"SECRET", "PASSWORD"} {
		if contains(string(raw), needle) {
			t.Fatal(string(raw))
		}
	}
	r := Route{APIKey: "SECRET"}
	raw, _ = json.Marshal(r)
	if contains(string(raw), "SECRET") {
		t.Fatal(string(raw))
	}
}
func contains(a, b string) bool {
	for i := 0; i+len(b) <= len(a); i++ {
		if a[i:i+len(b)] == b {
			return true
		}
	}
	return false
}
