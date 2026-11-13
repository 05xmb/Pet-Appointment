package httpapi

import (
	"strconv"
	"testing"
	"time"
)

func TestLoginLimiterTracksFailuresAndExpiresWindows(t *testing.T) {
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(func() time.Time { return now })
	if loginKey("127.0.0.1:8080", "owner") != "127.0.0.1\x00owner" {
		t.Fatal("host-port login key was not normalized")
	}
	if loginKey("unknown", "owner") != "unknown\x00owner" {
		t.Fatal("raw login key was not preserved")
	}
	for i := 0; i < 5; i++ {
		if !limiter.allow("127.0.0.1:8080", "owner") {
			t.Fatalf("attempt %d unexpectedly rejected", i+1)
		}
	}
	if limiter.allow("127.0.0.1:8080", "owner") {
		t.Fatal("sixth attempt accepted")
	}
	limiter.succeeded("127.0.0.1:8080", "owner")
	if !limiter.allow("127.0.0.1:8080", "owner") {
		t.Fatal("successful login did not clear window")
	}
	now = now.Add(2 * time.Minute)
	if !limiter.allow("127.0.0.1:8080", "owner") {
		t.Fatal("expired window was not reset")
	}
}

func TestLoginLimiterCleansExpiredEntriesAtCapacity(t *testing.T) {
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(func() time.Time { return now })
	for i := 0; i < 1024; i++ {
		if !limiter.allow("127.0.0.1:1000", "owner"+strconv.Itoa(i)) {
			t.Fatalf("entry %d unexpectedly rejected", i)
		}
	}
	now = now.Add(2 * time.Minute)
	if !limiter.allow("new-peer:9999", "owner") {
		t.Fatal("expired entries were not cleaned")
	}
}
