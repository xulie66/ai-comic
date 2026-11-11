package auth

import (
	"comic.local/go-service/internal/store"
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// BootstrapAdmin explicitly creates an administrator; it never promotes an
// existing account or resets a password as a side effect of service startup.
func (a *Service) BootstrapAdmin(ctx context.Context, username, password string) (store.PublicUser, error) {
	name := strings.TrimSpace(username)
	if !usernamePattern.MatchString(name) || utf8.RuneCountInString(password) < 12 || len(password) > 4096 {
		return store.PublicUser{}, store.ErrInvalid
	}
	if _, err := a.store.UserByName(ctx, name); err == nil {
		return store.PublicUser{}, store.ErrConflict
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.PublicUser{}, err
	}
	salt, hash, err := HashPassword(password)
	if err != nil {
		return store.PublicUser{}, err
	}
	u := store.User{ID: store.NewID("usr"), Username: strings.ToLower(name), DisplayName: name, Salt: salt, Hash: hash, IsAdmin: true, CreatedAt: time.Now().UTC(), CreditStatus: "active"}
	if err = a.store.CreateUser(ctx, u); err != nil {
		return store.PublicUser{}, err
	}
	return u.Public(), nil
}
