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

	"github.com/05xmb/Pet-Appointment/internal/slot"
)

type slotRepository struct {
	created   slot.Slot
	createErr error
	listed    []slot.Slot
	listErr   error
	calls     int
}

func (r *slotRepository) Create(_ context.Context, _, _, _ int64, _, _ time.Time, _ int) (slot.Slot, error) {
	r.calls++
	if r.createErr != nil {
		return slot.Slot{}, r.createErr
	}
	return r.created, nil
}
func (r *slotRepository) List(context.Context, int64, time.Time, time.Time) ([]slot.Slot, error) {
	return r.listed, r.listErr
}

func TestSlotHTTPManagerCanPublishAndOwnerCannot(t *testing.T) {
	repo := &slotRepository{created: slot.Slot{ID: 42, StoreID: 1, ProjectID: 1, Capacity: 3}}
	h, clock, accountRepo := testFixtureForRole(t, "manager")
	h = NewHandler(accountService(accountRepo), clock, WithSlotService(slot.NewService(repo)))
	cookie := loginCookie(t, h)
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().In(loc).Add(2 * time.Hour).Truncate(time.Minute)
	form := url.Values{"project_id": {"1"}, "start_at": {start.Format("2006-01-02T15:04")}, "end_at": {start.Add(time.Hour).Format("2006-01-02T15:04")}, "capacity": {"3"}}
	req := httptest.NewRequest(http.MethodPost, "https://example.test/slots", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || repo.calls != 1 {
		t.Fatalf("manager publish status=%d calls=%d body=%q", w.Code, repo.calls, w.Body.String())
	}
	var got slot.Slot
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil || got.ID != 42 {
		t.Fatalf("slot=%+v error=%v", got, err)
	}

	owner, ownerClock, ownerRepo := testFixtureForRole(t, "owner")
	owner = NewHandler(accountService(ownerRepo), ownerClock, WithSlotService(slot.NewService(&slotRepository{createErr: errors.New("must not call")})))
	ownerCookie := loginCookie(t, owner)
	req = httptest.NewRequest(http.MethodPost, "https://example.test/slots", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ownerCookie)
	w = httptest.NewRecorder()
	owner.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("owner publish status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestSlotHTTPListsAndRejectsInvalidInputs(t *testing.T) {
	repo := &slotRepository{listed: []slot.Slot{{ID: 1, StoreID: 1}}}
	h, clock, accountRepo := testFixtureForRole(t, "manager")
	h = NewHandler(accountService(accountRepo), clock, WithSlotService(slot.NewService(repo)))
	cookie := loginCookie(t, h)
	request := func(method, target string, form url.Values) *httptest.ResponseRecorder {
		return requestWithCookie(h, method, target, cookie, form)
	}
	if got := request(http.MethodGet, "https://example.test/slots?date=2026-11-13", nil); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"id":1`) {
		t.Fatalf("list status=%d body=%q", got.Code, got.Body.String())
	}
	if got := request(http.MethodGet, "https://example.test/slots?date=bad", nil); got.Code != http.StatusBadRequest {
		t.Fatalf("bad date status=%d", got.Code)
	}
	valid := url.Values{"project_id": {"1"}, "start_at": {"2099-01-01T10:00"}, "end_at": {"2099-01-01T11:00"}, "capacity": {"3"}}
	for field, value := range map[string]string{"project_id": "bad", "start_at": "bad", "end_at": "bad", "capacity": "bad"} {
		form := url.Values{}
		for key, values := range valid {
			form[key] = append([]string(nil), values...)
		}
		form.Set(field, value)
		if got := request(http.MethodPost, "https://example.test/slots", form); got.Code != http.StatusBadRequest {
			t.Fatalf("field=%s status=%d body=%q", field, got.Code, got.Body.String())
		}
	}
	clock.readErr = errors.New("clock failed")
	if got := request(http.MethodPost, "https://example.test/slots", valid); got.Code != http.StatusInternalServerError {
		t.Fatalf("clock failure status=%d", got.Code)
	}
	clock.readErr = nil
	for errValue, want := range map[error]int{slot.ErrInvalidTime: http.StatusBadRequest, slot.ErrInvalidCapacity: http.StatusBadRequest, slot.ErrNotFound: http.StatusNotFound, errors.New("db failed"): http.StatusInternalServerError} {
		repo.createErr = errValue
		if got := request(http.MethodPost, "https://example.test/slots", valid); got.Code != want {
			t.Fatalf("error=%v status=%d want=%d", errValue, got.Code, want)
		}
	}
	repo.listErr = errors.New("db failed")
	if got := request(http.MethodGet, "https://example.test/slots?date=2026-11-13", nil); got.Code != http.StatusInternalServerError {
		t.Fatalf("list failure status=%d", got.Code)
	}
}
