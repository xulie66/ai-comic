package auth

import (
	"comic.local/go-service/internal/store"
	"context"
	"testing"
)

func TestExplicitAdminBootstrap(t *testing.T) {
	_, a := setup(t)
	ctx := context.Background()
	if _, err := a.BootstrapAdmin(ctx, "admin", ""); err != store.ErrInvalid {
		t.Fatal(err)
	}
	u, err := a.BootstrapAdmin(ctx, "admin", "long-password-123")
	if err != nil || !u.IsAdmin {
		t.Fatalf("bootstrap: %+v %v", u, err)
	}
	if _, err = a.BootstrapAdmin(ctx, "admin", "replacement-password"); err != store.ErrConflict {
		t.Fatal("existing admin altered", err)
	}
	if _, err = a.Login(ctx, Credentials{Username: "admin", Password: "long-password-123"}); err != nil {
		t.Fatal(err)
	}
	ordinary, err := a.Register(ctx, Credentials{Username: "normal", Password: "secret123"})
	if err != nil || ordinary.User.IsAdmin {
		t.Fatal("public signup gained admin", err)
	}
}
