package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	Addr, DataRoot, DatabaseURL, CookieName, PublicDir string
	SecureCookies                                      bool
	SessionTTL                                         time.Duration
	MaxBodyBytes                                       int64
}

func Load() (Config, error) {
	c := Config{Addr: env("GO_SERVICE_ADDR", "127.0.0.1:5210"), DataRoot: env("GO_SERVICE_DATA_ROOT", "./data"), DatabaseURL: env("GO_SERVICE_DATABASE_URL", os.Getenv("DATABASE_URL")), CookieName: env("GO_SERVICE_COOKIE_NAME", "ac_session"), PublicDir: env("GO_SERVICE_PUBLIC_DIR", "../web/public"), SessionTTL: 30 * 24 * time.Hour, MaxBodyBytes: 16 << 20}
	var err error
	c.DataRoot, err = filepath.Abs(c.DataRoot)
	if err != nil {
		return c, err
	}
	if v := os.Getenv("GO_SERVICE_SECURE_COOKIES"); v != "" {
		c.SecureCookies, err = strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("GO_SERVICE_SECURE_COOKIES: %w", err)
		}
	}
	if v := os.Getenv("GO_SERVICE_SESSION_TTL"); v != "" {
		c.SessionTTL, err = time.ParseDuration(v)
		if err != nil || c.SessionTTL <= 0 {
			return c, fmt.Errorf("invalid GO_SERVICE_SESSION_TTL")
		}
	}
	if v := os.Getenv("GO_SERVICE_MAX_BODY_BYTES"); v != "" {
		c.MaxBodyBytes, err = strconv.ParseInt(v, 10, 64)
		if err != nil || c.MaxBodyBytes < 1024 {
			return c, fmt.Errorf("invalid GO_SERVICE_MAX_BODY_BYTES")
		}
	}
	return c, nil
}
func env(k, d string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return d
}
