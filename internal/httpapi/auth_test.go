package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

type repository struct {
	account    auth.Account
	session    map[[32]byte]auth.Session
	lookupErr  error
	saveErr    error
	sessionErr error
	deleteErr  error
}

type clockRepository struct {
	offset     time.Duration
	readErr    error
	advanceErr error
}

func (c *clockRepository) ClockNow(_ context.Context, realNow time.Time) (time.Time, error) {
	if c.readErr != nil {
		return time.Time{}, c.readErr
	}
	return realNow.Add(c.offset), nil
}

func (c *clockRepository) AdvanceClock(_ context.Context, minutes int64) error {
	if c.advanceErr != nil {
		return c.advanceErr
	}
	if minutes <= 0 {
		return errors.New("invalid advance")
	}
	c.offset += time.Duration(minutes) * time.Minute
	return nil
}

func (r *repository) AccountByUsername(_ context.Context, name string) (auth.Account, error) {
	if r.lookupErr != nil {
		return auth.Account{}, r.lookupErr
	}
	if name != r.account.Username {
		return auth.Account{}, auth.ErrNotFound
	}
	return r.account, nil
}

func (r *repository) SaveSession(_ context.Context, digest [32]byte, _ int64, _ string, expires time.Time) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.session[digest] = auth.Session{Account: r.account, ExpiresAt: expires}
	return nil
}

func (r *repository) SessionByDigest(_ context.Context, digest [32]byte) (auth.Session, error) {
	if r.sessionErr != nil {
		return auth.Session{}, r.sessionErr
	}
	s, ok := r.session[digest]
	if !ok {
		return auth.Session{}, auth.ErrNotFound
	}
	return s, nil
}

func (r *repository) DeleteSession(_ context.Context, digest [32]byte) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.session, digest)
	return nil
}

func testHandler(t *testing.T) http.Handler {
	h, _ := testHandlerForRole(t, "owner")
	return h
}

func testHandlerForRole(t *testing.T, role string) (http.Handler, *clockRepository) {
	h, clock, _ := testFixtureForRole(t, role)
	return h, clock
}

func testFixtureForRole(t *testing.T, role string) (http.Handler, *clockRepository, *repository) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := &repository{account: auth.Account{ID: 7, Username: "owner", PasswordHash: string(hash), Role: role, StoreID: 1, OwnerID: 7}, session: make(map[[32]byte]auth.Session)}
	clock := &clockRepository{}
	return NewHandler(auth.NewService(repo, time.Now), clock), clock, repo
}

func loginRequest(scheme string) *http.Request {
	form := url.Values{"username": {"owner"}, "password": {"test-password"}}
	req := httptest.NewRequest(http.MethodPost, scheme+"://example.test/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestLoginMeAndLogoutUseServerSession(t *testing.T) {
	h := testHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, loginRequest("https"))
	if w.Code != http.StatusNoContent {
		t.Fatalf("login status=%d body=%q", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == "" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("insecure session cookie: %+v", cookies)
	}

	me := httptest.NewRequest(http.MethodGet, "https://example.test/me?role=manager&user_id=999", nil)
	me.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, me)
	var identity auth.Identity
	if w.Code != http.StatusOK || json.NewDecoder(w.Body).Decode(&identity) != nil || identity.ID != 7 || identity.Role != "owner" || identity.OwnerID != 7 {
		t.Fatalf("me status=%d identity=%+v body=%q", w.Code, identity, w.Body.String())
	}

	logout := httptest.NewRequest(http.MethodPost, "https://example.test/logout", nil)
	logout.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, logout)
	if w.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, me)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status=%d", w.Code)
	}
}

func TestLoginRejectsPlainHTTPAndCrossOrigin(t *testing.T) {
	h := testHandler(t)
	for _, req := range []*http.Request{loginRequest("http"), loginRequest("https")} {
		if req.TLS != nil {
			req.Header.Set("Origin", "https://untrusted.example")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s login status=%d, want forbidden", req.URL.Scheme, w.Code)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("rejected request set session cookie")
		}
	}
}

func TestWrongPasswordAndMissingSessionAreUnauthorized(t *testing.T) {
	h := testHandler(t)
	badForm := url.Values{"username": {"owner"}, "password": {"wrong"}}.Encode()
	bad := httptest.NewRequest(http.MethodPost, "https://example.test/login", strings.NewReader(badForm))
	bad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, bad)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("empty password status=%d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "https://example.test/me", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status=%d", w.Code)
	}
}

func TestLoginReturnsServerErrorWhenAccountStoreUnavailable(t *testing.T) {
	h, _, repo := testFixtureForRole(t, "owner")
	repo.lookupErr = errors.New("database unavailable")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, loginRequest("https"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("login store failure status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestOnlyManagerCanAdvanceTeachingClock(t *testing.T) {
	for _, role := range []string{"owner", "manager"} {
		h, clock := testHandlerForRole(t, role)
		login := httptest.NewRecorder()
		h.ServeHTTP(login, loginRequest("https"))
		if login.Code != http.StatusNoContent {
			t.Fatalf("%s login status=%d", role, login.Code)
		}
		cookie := login.Result().Cookies()[0]
		advance := func(crossOrigin bool, minutes string) int {
			body := url.Values{"minutes": {minutes}}.Encode()
			req := httptest.NewRequest(http.MethodPost, "https://example.test/clock/advance?role=manager", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if crossOrigin {
				req.Header.Set("Origin", "https://untrusted.example")
				req.Header.Set("Sec-Fetch-Site", "cross-site")
			}
			req.AddCookie(cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			return w.Code
		}
		if got := advance(true, "5"); got != http.StatusForbidden {
			t.Fatalf("%s cross-site advance status=%d", role, got)
		}
		if role == "owner" {
			if got := advance(false, "5"); got != http.StatusForbidden || clock.offset != 0 {
				t.Fatalf("owner advanced clock: status=%d offset=%s", got, clock.offset)
			}
			continue
		}
		if got := advance(false, "0"); got != http.StatusBadRequest {
			t.Fatalf("invalid advance status=%d", got)
		}
		if got := advance(false, "5"); got != http.StatusNoContent || clock.offset != 5*time.Minute {
			t.Fatalf("manager advance status=%d offset=%s", got, clock.offset)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "https://example.test/clock", nil)
		req.AddCookie(cookie)
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "+08:00") {
			t.Fatalf("clock status=%d body=%q", w.Code, w.Body.String())
		}
	}
}

func TestClockHTTPReportsCurrentTimeAndFailures(t *testing.T) {
	h, clock := testHandlerForRole(t, "manager")
	cookie := loginCookie(t, h)
	request := httptest.NewRequest(http.MethodGet, "https://example.test/clock", nil)
	request.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code != http.StatusOK {
		t.Fatalf("clock status=%d body=%q", w.Code, w.Body.String())
	}
	clock.readErr = errors.New("clock failed")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("clock failure status=%d body=%q", w.Code, w.Body.String())
	}
	clock.readErr = nil
	clock.advanceErr = errors.New("clock advance failed")
	advance := httptest.NewRequest(http.MethodPost, "https://example.test/clock/advance", strings.NewReader("minutes=5"))
	advance.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	advance.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, advance)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("clock advance failure status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestLoginLimitsRepeatedGuessesByPeerAddress(t *testing.T) {
	h := testHandler(t)
	guess := func(remote, password string) int {
		form := url.Values{"username": {"owner"}, "password": {password}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "https://example.test/login", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = remote
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	for i := 0; i < 5; i++ {
		if got := guess("192.0.2.1:1234", "wrong"); got != http.StatusUnauthorized {
			t.Fatalf("guess %d status=%d", i+1, got)
		}
	}
	if got := guess("192.0.2.1:4321", "test-password"); got != http.StatusTooManyRequests {
		t.Fatalf("rate-limited peer status=%d", got)
	}
	if got := guess("192.0.2.2:1234", "test-password"); got != http.StatusNoContent {
		t.Fatalf("other peer status=%d", got)
	}
}

func TestLoginLimiterWindowExpires(t *testing.T) {
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(func() time.Time { return now })
	for i := 0; i < 5; i++ {
		if !limiter.allow("192.0.2.1:1234", "owner") {
			t.Fatalf("attempt %d rejected early", i+1)
		}
	}
	if limiter.allow("192.0.2.1:4321", "owner") {
		t.Fatal("sixth attempt from same peer accepted")
	}
	now = now.Add(time.Minute)
	if !limiter.allow("192.0.2.1:1234", "owner") {
		t.Fatal("peer remained blocked after window expired")
	}
}

func TestSuccessfulLoginsDoNotExhaustPeerLimit(t *testing.T) {
	h := testHandler(t)
	for i := 0; i < 8; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, loginRequest("https"))
		if w.Code != http.StatusNoContent {
			t.Fatalf("successful login %d status=%d", i+1, w.Code)
		}
	}
}

func TestHTTPFailuresDoNotLeakSessionsOrChangeClock(t *testing.T) {
	h, clock, repo := testFixtureForRole(t, "manager")
	dbDown := errors.New("database unavailable")
	check := func(req *http.Request, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s status=%d body=%q, want %d", req.Method, req.URL.Path, w.Code, w.Body.String(), want)
		}
		return w
	}
	repo.lookupErr = dbDown
	if got := check(loginRequest("https"), http.StatusInternalServerError); len(got.Result().Cookies()) != 0 {
		t.Fatal("failed login set cookie")
	}
	repo.lookupErr = nil
	repo.saveErr = dbDown
	check(loginRequest("https"), http.StatusInternalServerError)
	repo.saveErr = nil
	login := check(loginRequest("https"), http.StatusNoContent)
	cookie := login.Result().Cookies()[0]
	requestWithSession := func(method, path string) *http.Request {
		req := httptest.NewRequest(method, "https://example.test"+path, nil)
		req.AddCookie(cookie)
		return req
	}
	repo.sessionErr = dbDown
	check(requestWithSession(http.MethodGet, "/me"), http.StatusInternalServerError)
	repo.sessionErr = nil
	repo.deleteErr = dbDown
	check(requestWithSession(http.MethodPost, "/logout"), http.StatusInternalServerError)
	repo.deleteErr = nil
	clock.readErr = dbDown
	check(requestWithSession(http.MethodGet, "/clock"), http.StatusInternalServerError)
	clock.readErr = nil
	clock.advanceErr = dbDown
	check(requestWithSession(http.MethodPost, "/clock/advance?minutes=5"), http.StatusBadRequest)
	body := url.Values{"minutes": {"5"}}.Encode()
	advance := httptest.NewRequest(http.MethodPost, "https://example.test/clock/advance", strings.NewReader(body))
	advance.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	advance.AddCookie(cookie)
	check(advance, http.StatusInternalServerError)
	if clock.offset != 0 {
		t.Fatal("failed clock advance changed offset")
	}
	check(httptest.NewRequest(http.MethodGet, "https://example.test/clock", nil), http.StatusUnauthorized)
	check(httptest.NewRequest(http.MethodPost, "https://example.test/logout", nil), http.StatusUnauthorized)
	for _, path := range []string{"/me", "/clock"} {
		check(httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil), http.StatusForbidden)
	}
	check(httptest.NewRequest(http.MethodPost, "http://example.test/logout", nil), http.StatusForbidden)
	check(httptest.NewRequest(http.MethodPost, "http://example.test/clock/advance", nil), http.StatusForbidden)
	large := httptest.NewRequest(http.MethodPost, "https://example.test/login", strings.NewReader(strings.Repeat("x", 5000)))
	large.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	check(large, http.StatusBadRequest)
}
