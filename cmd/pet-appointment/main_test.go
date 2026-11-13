package main

import (
	"net/http"
	"testing"
)

func TestServeRequiresTLSFilesAndDatabaseDSN(t *testing.T) {
	for _, env := range []map[string]string{
		{},
		{"PET_MYSQL_DSN": "user@tcp(localhost:3306)/pet"},
		{"PET_MYSQL_DSN": "user@tcp(localhost:3306)/pet", "PET_TLS_CERT": "cert.pem"},
	} {
		if _, err := parseConfig([]string{"serve"}, func(name string) string { return env[name] }); err == nil {
			t.Fatalf("serve accepted incomplete settings: cert=%q key=%q", env["PET_TLS_CERT"], env["PET_TLS_KEY"])
		}
	}
	env := map[string]string{"PET_MYSQL_DSN": "user@tcp(localhost:3306)/pet", "PET_TLS_CERT": "cert.pem", "PET_TLS_KEY": "key.pem", "PET_PAYMENT_HMAC_SECRET": "test-secret"}
	cfg, err := parseConfig([]string{"serve"}, func(name string) string { return env[name] })
	if err != nil || cfg.addr != "127.0.0.1:8443" {
		t.Fatalf("serve config address=%q error=%v", cfg.addr, err)
	}
}

func TestInitAndPasswordSetupNeedOnlyDatabaseDSN(t *testing.T) {
	getenv := func(name string) string {
		if name == "PET_MYSQL_DSN" {
			return "user@tcp(localhost:3306)/pet"
		}
		return ""
	}
	if _, err := parseConfig([]string{"init"}, getenv); err != nil {
		t.Fatalf("init config: %v", err)
	}
	cfg, err := parseConfig([]string{"set-password", "owner1"}, getenv)
	if err != nil || cfg.username != "owner1" {
		t.Fatalf("password setup username=%q error=%v", cfg.username, err)
	}
	if _, err := parseConfig([]string{"set-password"}, getenv); err == nil {
		t.Fatal("missing username accepted")
	}
}

func TestServerBoundsHeaderBodyAndResponseTime(t *testing.T) {
	server := newServer(config{addr: "127.0.0.1:8443"}, http.NewServeMux())
	if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= server.ReadHeaderTimeout || server.WriteTimeout <= 0 {
		t.Fatalf("server timeouts: header=%s read=%s write=%s", server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout)
	}
}
