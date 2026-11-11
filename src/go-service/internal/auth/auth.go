package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"comic.local/go-service/internal/store"
	"golang.org/x/crypto/pbkdf2"
)

var ErrCredentials = errors.New("invalid credentials")
var ErrUnauthenticated = errors.New("authentication required")
var ErrDisabled = errors.New("account disabled")

type Options struct {
	CookieName    string
	SecureCookies bool
	SessionTTL    time.Duration
}
type Service struct {
	store *store.Store
	opts  Options
}

func New(s *store.Store, o Options) *Service {
	if o.CookieName == "" {
		o.CookieName = "ac_session"
	}
	if o.SessionTTL <= 0 {
		o.SessionTTL = 30 * 24 * time.Hour
	}
	return &Service{s, o}
}

type Credentials struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}
type Result struct {
	User      store.PublicUser `json:"user"`
	Token     string           `json:"token"`
	ExpiresAt int64            `json:"expiresAt"`
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_\x{4e00}-\x{9fa5}-]{2,24}$`)

func HashPassword(password string) (salt, hash string, err error) {
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return
	}
	salt = hex.EncodeToString(b)
	hash = hex.EncodeToString(pbkdf2.Key([]byte(password), b, 120000, 32, sha256.New))
	return
}
func VerifyPassword(password, saltHex, hashHex string) bool {
	salt, e1 := hex.DecodeString(saltHex)
	expected, e2 := hex.DecodeString(hashHex)
	if e1 != nil || e2 != nil || len(salt) == 0 || len(expected) != 32 || len(password) > 4096 {
		return false
	}
	derived := pbkdf2.Key([]byte(password), salt, 120000, 32, sha256.New)
	return subtle.ConstantTimeCompare(derived, expected) == 1
}
func (a *Service) Register(ctx context.Context, c Credentials) (Result, error) {
	name := strings.TrimSpace(c.Username)
	if !usernamePattern.MatchString(name) || utf8.RuneCountInString(c.Password) < 6 || len(c.Password) > 4096 || utf8.RuneCountInString(c.DisplayName) > 120 {
		return Result{}, store.ErrInvalid
	}
	salt, hash, err := HashPassword(c.Password)
	if err != nil {
		return Result{}, err
	}
	display := strings.TrimSpace(c.DisplayName)
	if display == "" {
		display = name
	}
	u := store.User{ID: store.NewID("usr"), Username: strings.ToLower(name), DisplayName: display, Salt: salt, Hash: hash, CreatedAt: time.Now().UTC(), CreditStatus: "active"}
	if err = a.store.CreateUser(ctx, u); err != nil {
		return Result{}, err
	}
	return a.newSession(ctx, u)
}
func (a *Service) Login(ctx context.Context, c Credentials) (Result, error) {
	if len(c.Password) > 4096 {
		return Result{}, ErrCredentials
	}
	u, err := a.store.UserByName(ctx, c.Username)
	if errors.Is(err, store.ErrNotFound) {
		VerifyPassword(c.Password, "00000000000000000000000000000000", strings.Repeat("00", 32))
		return Result{}, ErrCredentials
	}
	if err != nil {
		return Result{}, err
	}
	if !VerifyPassword(c.Password, u.Salt, u.Hash) {
		return Result{}, ErrCredentials
	}
	if u.CreditStatus == "banned" {
		return Result{}, ErrDisabled
	}
	if err = a.store.TouchLogin(ctx, u.ID); err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	u.LastLoginAt = &now
	return a.newSession(ctx, u)
}
func (a *Service) newSession(ctx context.Context, u store.User) (Result, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return Result{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	expires := time.Now().Add(a.opts.SessionTTL).UnixMilli()
	if err := a.store.PutSession(ctx, store.Session{Token: token, UserID: u.ID, ExpiresAt: expires}); err != nil {
		return Result{}, err
	}
	return Result{u.Public(), token, expires}, nil
}
func (a *Service) Session(ctx context.Context, token string) (store.PublicUser, error) {
	if token == "" || len(token) > 512 {
		return store.PublicUser{}, ErrUnauthenticated
	}
	v, err := a.store.GetSession(ctx, token)
	if errors.Is(err, store.ErrNotFound) {
		return store.PublicUser{}, ErrUnauthenticated
	}
	if err != nil {
		return store.PublicUser{}, err
	}
	u, err := a.store.UserByID(ctx, v.UserID)
	if errors.Is(err, store.ErrNotFound) {
		return store.PublicUser{}, ErrUnauthenticated
	}
	if err != nil {
		return store.PublicUser{}, err
	}
	if u.CreditStatus == "banned" {
		return store.PublicUser{}, ErrDisabled
	}
	return u.Public(), nil
}

// Explicit Authorization always wins; malformed/expired Bearer never falls back
// to a cookie belonging to another user. Provider API keys are never accepted.
func (a *Service) Token(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	if len(values) > 0 {
		if len(values) != 1 {
			return "", ErrUnauthenticated
		}
		p := strings.Fields(values[0])
		if len(p) != 2 || !strings.EqualFold(p[0], "Bearer") {
			return "", ErrUnauthenticated
		}
		return p[1], nil
	}
	cookie, err := r.Cookie(a.opts.CookieName)
	if err != nil {
		return "", ErrUnauthenticated
	}
	return cookie.Value, nil
}
func (a *Service) Authenticate(r *http.Request) (store.PublicUser, error) {
	token, err := a.Token(r)
	if err != nil {
		return store.PublicUser{}, err
	}
	return a.Session(r.Context(), token)
}
func (a *Service) Logout(ctx context.Context, token string) error {
	return a.store.DeleteSession(ctx, token)
}
func (a *Service) SetCookie(w http.ResponseWriter, result Result) {
	http.SetCookie(w, &http.Cookie{Name: a.opts.CookieName, Value: result.Token, Path: "/", HttpOnly: true, Secure: a.opts.SecureCookies, SameSite: http.SameSiteLaxMode, Expires: time.UnixMilli(result.ExpiresAt), MaxAge: int(a.opts.SessionTTL.Seconds())})
}
func (a *Service) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: a.opts.CookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.opts.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func (a *Service) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) (Result, error) {
	if utf8.RuneCountInString(newPassword) < 6 || len(newPassword) > 4096 {
		return Result{}, store.ErrInvalid
	}
	u, err := a.store.UserByID(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	if !VerifyPassword(oldPassword, u.Salt, u.Hash) {
		return Result{}, ErrCredentials
	}
	salt, hash, err := HashPassword(newPassword)
	if err != nil {
		return Result{}, err
	}
	if err = a.store.ChangePassword(ctx, userID, salt, hash); err != nil {
		return Result{}, err
	}
	return a.newSession(ctx, u)
}

type contextKey struct{}

func WithUser(ctx context.Context, u store.PublicUser) context.Context {
	return context.WithValue(ctx, contextKey{}, u)
}
func UserFromContext(ctx context.Context) (store.PublicUser, bool) {
	u, ok := ctx.Value(contextKey{}).(store.PublicUser)
	return u, ok
}
