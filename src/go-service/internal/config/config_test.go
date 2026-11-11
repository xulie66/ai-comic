package config

import "testing"

func TestDefaultsAndValidation(t *testing.T) {
	for _, k := range []string{"GO_SERVICE_ADDR", "GO_SERVICE_DATA_ROOT", "GO_SERVICE_DATABASE_URL", "DATABASE_URL", "GO_SERVICE_COOKIE_NAME", "GO_SERVICE_PUBLIC_DIR", "GO_SERVICE_SECURE_COOKIES", "GO_SERVICE_SESSION_TTL", "GO_SERVICE_MAX_BODY_BYTES"} {
		t.Setenv(k, "")
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:5210" || c.CookieName != "ac_session" || c.DatabaseURL != "" {
		t.Fatalf("defaults: %+v", c)
	}
	for _, pair := range [][2]string{{"GO_SERVICE_SESSION_TTL", "-1s"}, {"GO_SERVICE_SECURE_COOKIES", "invalid"}, {"GO_SERVICE_MAX_BODY_BYTES", "4"}} {
		t.Setenv(pair[0], pair[1])
		if _, err = Load(); err == nil {
			t.Fatal("accepted invalid", pair)
		}
		t.Setenv(pair[0], "")
	}
}
