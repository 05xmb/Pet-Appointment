package httpapi

import (
	"net"
	"sync"
	"time"
)

type loginWindow struct {
	count   int
	expires time.Time
}

type loginLimiter struct {
	mu     sync.Mutex
	window map[string]loginWindow
	now    func() time.Time
}

func newLoginLimiter(now func() time.Time) *loginLimiter {
	return &loginLimiter{window: make(map[string]loginWindow), now: now}
}

func loginKey(remote, username string) string {
	peer, _, err := net.SplitHostPort(remote)
	if err != nil {
		peer = remote
	}
	return peer + "\x00" + username
}

func (l *loginLimiter) allow(remote, username string) bool {
	key := loginKey(remote, username)
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, known := l.window[key]
	if !known || !now.Before(entry.expires) {
		if !known && len(l.window) >= 1024 {
			for address, old := range l.window {
				if !now.Before(old.expires) {
					delete(l.window, address)
				}
			}
			if len(l.window) >= 1024 {
				return false
			}
		}
		entry = loginWindow{expires: now.Add(time.Minute)}
	}
	if entry.count >= 5 {
		return false
	}
	entry.count++
	l.window[key] = entry
	return true
}

func (l *loginLimiter) succeeded(remote, username string) {
	l.mu.Lock()
	delete(l.window, loginKey(remote, username))
	l.mu.Unlock()
}
