package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func cliTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("PET_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("PET_MYSQL_TEST_DSN not set; CLI integration test not run")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.Ping(); err != nil {
		t.Fatal("test MySQL connection failed")
	}
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "pet_appointment_test_" + hex.EncodeToString(suffix[:])
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
			t.Errorf("could not remove isolated test database: %v", err)
		}
	})
	cfg.DBName = name
	return cfg.FormatDSN()
}

func TestCLIInitializesAndRequiresAllPasswordsBeforeServing(t *testing.T) {
	t.Setenv("PET_MYSQL_DSN", cliTestDSN(t))
	t.Setenv("PET_TLS_CERT", "missing-cert.pem")
	t.Setenv("PET_TLS_KEY", "missing-key.pem")
	t.Setenv("PET_PAYMENT_HMAC_SECRET", "test-secret")
	t.Setenv("PET_LISTEN_ADDR", "127.0.0.1:0")
	if err := run([]string{"init"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"init"}); err != nil {
		t.Fatalf("repeat init: %v", err)
	}
	if err := run([]string{"serve"}); err == nil || !strings.Contains(err.Error(), "set passwords") {
		t.Fatalf("unconfigured serve error=%v", err)
	}
	if err := runWithPrompt([]string{"set-password", "owner1"}, func(string) ([]byte, error) { return nil, errors.New("prompt failed") }); err == nil {
		t.Fatal("password prompt failure ignored")
	}
	if err := runWithPrompt([]string{"set-password", "missing"}, func(string) ([]byte, error) { return []byte("fixture-secret"), nil }); err == nil {
		t.Fatal("missing account password update accepted")
	}
	for _, name := range []string{"owner1", "owner2", "owner3", "owner4", "frontdesk", "groomer", "support", "manager"} {
		prompt := func(username string) ([]byte, error) { return []byte(username + "-fixture-secret"), nil }
		if err := runWithPrompt([]string{"set-password", name}, prompt); err != nil {
			t.Fatalf("provision %s: %v", name, err)
		}
	}
	if err := run([]string{"serve"}); err == nil || strings.Contains(err.Error(), "set passwords") {
		t.Fatalf("fully configured serve error=%v, want missing certificate", err)
	}
}

func TestPasswordConfirmationDoesNotExposeOrAcceptMismatch(t *testing.T) {
	var output bytes.Buffer
	inputs := [][]byte{[]byte("fixture-secret"), []byte("fixture-secret")}
	read := func() ([]byte, error) {
		value := inputs[0]
		inputs = inputs[1:]
		return value, nil
	}
	password, err := confirmPassword("owner1", read, &output)
	if err != nil || string(password) != "fixture-secret" || strings.Contains(output.String(), "fixture-secret") {
		t.Fatalf("confirmed password valid=%v output leaked=%v error=%v", string(password) == "fixture-secret", strings.Contains(output.String(), "fixture-secret"), err)
	}
	clear(password)
	inputs = [][]byte{[]byte("first-secret"), []byte("different-secret")}
	if _, err := confirmPassword("owner1", read, &output); err == nil {
		t.Fatal("mismatched confirmation accepted")
	}
	if _, err := confirmPassword("owner1", func() ([]byte, error) { return nil, errors.New("terminal read failed") }, &output); err == nil {
		t.Fatal("terminal read failure ignored")
	}
}
