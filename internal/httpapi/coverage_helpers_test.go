package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/cancellation"
	"github.com/05xmb/Pet-Appointment/internal/fulfillment"
	"github.com/05xmb/Pet-Appointment/internal/payment"
	"github.com/05xmb/Pet-Appointment/internal/reporting"
)

func TestHTTPErrorMappings(t *testing.T) {
	testMapping := func(name string, errorsToStatus map[error]int, write func(http.ResponseWriter, error)) {
		t.Helper()
		for err, want := range errorsToStatus {
			w := httptest.NewRecorder()
			write(w, err)
			if w.Code != want {
				t.Errorf("%s error=%v status=%d want=%d", name, err, w.Code, want)
			}
		}
	}
	testMapping("payment", map[error]int{
		payment.ErrInvalidSignature:      http.StatusUnauthorized,
		payment.ErrInvalidCallback:       http.StatusBadRequest,
		payment.ErrPaymentNotFound:       http.StatusNotFound,
		payment.ErrPaymentAmountMismatch: http.StatusConflict,
		payment.ErrBookingNotPayable:     http.StatusConflict,
		booking.ErrForbidden:             http.StatusForbidden,
		booking.ErrNotFound:              http.StatusNotFound,
	}, writePaymentError)
	testMapping("booking", map[error]int{
		booking.ErrForbidden:    http.StatusForbidden,
		booking.ErrNotFound:     http.StatusNotFound,
		booking.ErrPetNotFound:  http.StatusNotFound,
		booking.ErrDuplicatePet: http.StatusConflict,
		booking.ErrCapacityFull: http.StatusConflict,
		booking.ErrSlotStarted:  http.StatusConflict,
		booking.ErrInvalidState: http.StatusConflict,
		booking.ErrNotExpired:   http.StatusConflict,
	}, writeBookingError)
	testMapping("cancellation", map[error]int{
		cancellation.ErrInvalidSignature: http.StatusUnauthorized,
		cancellation.ErrInvalidCallback:  http.StatusBadRequest,
		cancellation.ErrForbidden:        http.StatusForbidden,
		cancellation.ErrInvalidState:     http.StatusConflict,
		cancellation.ErrTooEarly:         http.StatusConflict,
		cancellation.ErrTooLate:          http.StatusConflict,
		booking.ErrNotFound:              http.StatusNotFound,
	}, writeCancellationError)
	testMapping("fulfillment", map[error]int{
		fulfillment.ErrForbidden:            http.StatusForbidden,
		fulfillment.ErrNotFound:             http.StatusNotFound,
		fulfillment.ErrInvalidState:         http.StatusConflict,
		fulfillment.ErrOutsideCheckInWindow: http.StatusConflict,
		fulfillment.ErrTooEarly:             http.StatusConflict,
		fulfillment.ErrReasonRequired:       http.StatusBadRequest,
	}, writeFulfillmentError)
}

func TestHTTPParsingHelpersRejectInvalidValues(t *testing.T) {
	if petID, slotID, err := parseIDs("1", "2"); err != nil || petID != 1 || slotID != 2 {
		t.Fatalf("valid ids pet=%d slot=%d error=%v", petID, slotID, err)
	}
	if _, _, err := parseIDs("", "1"); err == nil {
		t.Fatal("empty pet accepted")
	}
	if _, _, err := parseIDs("1", "0"); err == nil {
		t.Fatal("zero slot accepted")
	}
	if _, _, err := parseIDs("bad", "1"); err == nil {
		t.Fatal("invalid pet accepted")
	}
	request := httptest.NewRequest(http.MethodGet, "https://example.test/bookings/bad", nil)
	if _, err := parsePathID(request, "booking"); err == nil {
		t.Fatal("invalid path id accepted")
	}
	request.SetPathValue("id", "41")
	if id, err := parsePathID(request, "booking"); err != nil || id != 41 {
		t.Fatalf("valid path id=%d error=%v", id, err)
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := reportDayRange("", loc); !errors.Is(err, reporting.ErrInvalidDate) {
		t.Fatalf("empty report date error=%v", err)
	}
	if _, _, _, err := reportDayRange("not-a-date", loc); err == nil {
		t.Fatal("invalid report date accepted")
	}
}
