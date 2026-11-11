package auth

import (
	"comic.local/go-service/internal/store"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func setup(t *testing.T) (*store.Store, *Service) {
	t.Helper()
	s, err := store.Open(context.Background(), store.Options{DataRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, New(s, Options{CookieName: "session", SessionTTL: time.Hour})
}
func TestLoginSessionIsolationAndPasswordChange(t *testing.T) {
	ctx := context.Background()
	_, a := setup(t)
	u, err := a.Register(ctx, Credentials{Username: "Alice", Password: "secret123"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := a.Register(ctx, Credentials{Username: "Bob", Password: "secret456"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Login(ctx, Credentials{Username: "alice", Password: "wrong"}); err != ErrCredentials {
		t.Fatalf("bad password: %v", err)
	}
	logged, err := a.Login(ctx, Credentials{Username: " ALICE ", Password: "secret123"})
	if err != nil || logged.User.ID != u.User.ID {
		t.Fatalf("login: %v", err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: u.Token})
	r.Header.Set("Authorization", "Bearer "+b.Token)
	got, err := a.Authenticate(r)
	if err != nil || got.ID != b.User.ID {
		t.Fatalf("bearer isolation: %+v %v", got, err)
	}
	r.Header.Set("Authorization", "Bearer expired")
	if _, err = a.Authenticate(r); err != ErrUnauthenticated {
		t.Fatalf("bearer fell back to cookie: %v", err)
	}
	r.Header.Set("Authorization", "Basic invalid")
	if _, err = a.Authenticate(r); err != ErrUnauthenticated {
		t.Fatal(err)
	}
	changed, err := a.ChangePassword(ctx, u.User.ID, "secret123", "newsecret123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Session(ctx, u.Token); err != ErrUnauthenticated {
		t.Fatalf("old session remains: %v", err)
	}
	if _, err = a.Session(ctx, logged.Token); err != ErrUnauthenticated {
		t.Fatal(err)
	}
	if _, err = a.Session(ctx, changed.Token); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Login(ctx, Credentials{Username: "alice", Password: "newsecret123"}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Session(ctx, b.Token); err != nil {
		t.Fatal("other user's session revoked", err)
	}
}
func TestPasswordNodeCompatibility(t *testing.T) { // Node crypto.pbkdf2Sync('password',Buffer.from('000102030405060708090a0b0c0d0e0f','hex'),120000,32,'sha256')
	const salt = "000102030405060708090a0b0c0d0e0f"
	const expected = "0a300026cc0ba55a240b894dd629b07d0fc4e091922bc91b7d711b092183a4f0"
	if !VerifyPassword("password", salt, expected) {
		t.Fatal("PBKDF2 does not match Node vector")
	}
	if VerifyPassword("wrong", salt, expected) {
		t.Fatal("accepted wrong password")
	}
	if VerifyPassword("password", salt, "aa") {
		t.Fatal("accepted malformed hash")
	}
}
func TestExpiredSession(t *testing.T) {
	ctx := context.Background()
	s, a := setup(t)
	u, err := a.Register(ctx, Credentials{Username: "Alice", Password: "secret123"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PutSession(ctx, store.Session{Token: "expired", UserID: u.User.ID, ExpiresAt: time.Now().Add(-time.Second).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Session(ctx, "expired"); err != ErrUnauthenticated {
		t.Fatal(err)
	}
}
