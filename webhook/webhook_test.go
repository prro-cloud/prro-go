package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prro-cloud/prro-go"
)

const secret = "whsec-test"

// envelope будує тіло доставки події event із даними data.
func envelope(event prro.WebhookEvent, data string) []byte {
	return fmt.Appendf(nil, `{"delivery_id":"0197a2c4-0000-7000-8000-000000000001","event":%q,`+
		`"attempt":2,"occurred_at":"2026-07-14T10:15:04Z","data":%s}`, event, data)
}

func TestSignVerify(t *testing.T) {
	body := envelope(prro.EventTaskCompleted, `{}`)
	sig := Sign(secret, body)
	if !strings.HasPrefix(sig, "sha256=") || len(sig) != len("sha256=")+64 {
		t.Fatalf("signature = %q", sig)
	}
	if err := Verify(secret, body, sig); err != nil {
		t.Fatal(err)
	}
	if err := Verify(secret, body, "sha256="+strings.ToUpper(sig[7:])); err != nil {
		t.Errorf("upper-case hex rejected: %v", err)
	}

	bad := map[string]struct {
		secret, sig string
		body        []byte
	}{
		"tampered body": {secret, sig, append(body, ' ')},
		"wrong secret":  {"other", sig, body},
		"no prefix":     {secret, sig[7:], body},
		"sha1 scheme":   {secret, "sha1=" + sig[7:], body},
		"not hex":       {secret, "sha256=zz", body},
		"empty":         {secret, "", body},
		"truncated":     {secret, sig[:20], body},
	}
	for name, tt := range bad {
		if err := Verify(tt.secret, tt.body, tt.sig); !errors.Is(err, ErrInvalidSignature) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestParse(t *testing.T) {
	body := envelope(prro.EventShiftOpened, `{"cash_register_id":"r1","shift_id":"s1","task_id":"t1","testing":true}`)
	e, err := Parse(secret, body, Sign(secret, body))
	if err != nil {
		t.Fatal(err)
	}
	if e.DeliveryID != "0197a2c4-0000-7000-8000-000000000001" || e.Type != prro.EventShiftOpened || e.Attempt != 2 ||
		!e.OccurredAt.Equal(time.Date(2026, 7, 14, 10, 15, 4, 0, time.UTC)) {
		t.Errorf("event = %+v", e)
	}
	d, err := e.Shift()
	if err != nil || d.ShiftID != "s1" || !d.Testing {
		t.Errorf("shift = %+v, err = %v", d, err)
	}
	if _, err := e.Task(); err == nil {
		t.Error("Task() on shift.opened: no error")
	}

	if _, err := Parse(secret, []byte(`{`), Sign(secret, []byte(`{`))); err == nil || errors.Is(err, ErrInvalidSignature) {
		t.Errorf("malformed envelope: err = %v", err)
	}
}

func TestEventData(t *testing.T) {
	tests := []struct {
		event prro.WebhookEvent
		data  string
		check func(t *testing.T, e *Event)
	}{
		{
			prro.EventTaskCompleted, `{"task_id":"t1","type":"receipt","status":"succeeded","cash_register_id":"r1",
			"result":{"receipt":{"document_id":"d1","local_number":42,"fiscal_number":"7466800082",
			"register_fiscal_number":"4001063533","receipt_url":"https://r.prro.cloud/aB3xK9pQvT2mNr7c"}}}`,
			func(t *testing.T, e *Event) {
				d, err := e.Task()
				if err != nil || d.Status != prro.TaskSucceeded || d.Result.Receipt.FiscalNumber != "7466800082" {
					t.Errorf("task = %+v, err = %v", d, err)
				}
			},
		},
		{
			prro.EventTaskFailed, `{"task_id":"t1","type":"receipt","status":"failed","error":"rejected",
			"fault":{"code":"dps.check_local_number_invalid","class":"resync"}}`,
			func(t *testing.T, e *Event) {
				d, err := e.Task()
				if err != nil || d.Fault.Class != prro.FaultResync || d.Result != nil {
					t.Errorf("task = %+v, err = %v", d, err)
				}
			},
		},
		{
			prro.EventKeyExpiring, `{"key_id":"k1","cash_register_ids":["r1","r2"],"cert_subject":"ФОП Іваненко",
			"cert_valid_to":"2026-08-01T00:00:00Z"}`,
			func(t *testing.T, e *Event) {
				d, err := e.Key()
				if err != nil || len(d.CashRegisterIDs) != 2 || d.CertValidTo.Month() != time.August {
					t.Errorf("key = %+v, err = %v", d, err)
				}
			},
		},
		{
			prro.EventRegisterOfflineLimit, `{"cash_register_id":"r1","session_id":"s1","dps_session_id":77,
			"started_at":"2026-07-13T00:00:00Z","session_used":"30h0m0s","month_used":"140h12m0s"}`,
			func(t *testing.T, e *Event) {
				d, err := e.Offline()
				if err != nil || d.SessionUsed != 30*time.Hour || d.MonthUsed != 140*time.Hour+12*time.Minute || d.DPSSessionID != 77 {
					t.Errorf("offline = %+v, err = %v", d, err)
				}
			},
		},
		{
			prro.EventRegisterOnline, `{"cash_register_id":"r1","session_id":"s1","dps_session_id":77,
			"started_at":"2026-07-13T00:00:00Z","ended_at":"2026-07-13T02:00:00Z","documents":15}`,
			func(t *testing.T, e *Event) {
				d, err := e.Offline()
				if err != nil || d.Documents != 15 || d.EndedAt.IsZero() || d.SessionUsed != 0 {
					t.Errorf("offline = %+v, err = %v", d, err)
				}
			},
		},
		{
			prro.EventRegisterOfflineAbandoned, `{"cash_register_id":"r1","session_id":"s1","dps_session_id":77,"task_id":"t9",
			"documents":[{"document_id":"d1","class":"check","kind":"sale","local_number":45,"total":"65.00","created_at":"2026-07-13T01:00:00Z"}]}`,
			func(t *testing.T, e *Event) {
				d, err := e.OfflineAbandoned()
				if err != nil || len(d.Documents) != 1 || d.Documents[0].Total != "65.00" {
					t.Errorf("abandoned = %+v, err = %v", d, err)
				}
			},
		},
		{
			prro.EventRegisterRemediated, `{"cash_register_id":"r1","task_id":"t1","action":"local_number_synced",
			"fault_code":"dps.check_local_number_invalid","details":{"old":41,"new":43}}`,
			func(t *testing.T, e *Event) {
				d, err := e.Remediation()
				if err != nil || d.Action != "local_number_synced" || d.Details["new"] != float64(43) {
					t.Errorf("remediation = %+v, err = %v", d, err)
				}
			},
		},
		{
			prro.EventRegisterNeedsAttention, `{"cash_register_id":"r1","reason":"offline_submission_paused","session_id":"s1","error":"rejected"}`,
			func(t *testing.T, e *Event) {
				d, err := e.Attention()
				if err != nil || d.Reason != prro.AttentionOfflineSubmissionPaused || d.SessionID != "s1" {
					t.Errorf("attention = %+v, err = %v", d, err)
				}
			},
		},
		{
			prro.EventClientBalanceLow, `{"balance":"5000.0000","soft_limit":"0.0000","price_per_receipt":"50.0000",
			"receipts_left":100,"threshold_receipts":200}`,
			func(t *testing.T, e *Event) {
				d, err := e.Balance()
				if err != nil || d.ReceiptsLeft != 100 || d.PricePerReceipt != "50.0000" {
					t.Errorf("balance = %+v, err = %v", d, err)
				}
			},
		},
		{
			prro.EventDPSOffline, `{"verdict":"offline","override":"","effective":"offline","cause":"probe_failed",
			"occurred_at":"2026-07-14T10:15:04Z"}`,
			func(t *testing.T, e *Event) {
				d, err := e.DPS()
				if err != nil || d.Effective != prro.DPSOffline {
					t.Errorf("dps = %+v, err = %v", d, err)
				}
			},
		},
		{
			prro.EventPing, `{"message":"test","webhook_id":"w1","sent_at":"2026-07-14T10:15:04Z"}`,
			func(t *testing.T, e *Event) {
				d, err := e.Ping()
				if err != nil || d.WebhookID != "w1" {
					t.Errorf("ping = %+v, err = %v", d, err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.event), func(t *testing.T) {
			body := envelope(tt.event, tt.data)
			e, err := Parse(secret, body, Sign(secret, body))
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, e)
		})
	}
}

func TestOfflineDataBadDuration(t *testing.T) {
	e := &Event{Type: prro.EventRegisterOfflineLimit, Data: []byte(`{"session_used":"forever"}`)}
	if _, err := e.Offline(); err == nil {
		t.Error("no error for malformed duration")
	}
}

func TestHandler(t *testing.T) {
	var got *Event
	h := Handler(secret, func(_ context.Context, e *Event) error {
		got = e
		if e.Type == prro.EventTaskFailed {
			return errors.New("db down")
		}
		return nil
	})

	send := func(method string, body []byte, sig string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), method, "/prro-hook", strings.NewReader(string(body)))
		if sig != "" {
			req.Header.Set(SignatureHeader, sig)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	ok := envelope(prro.EventTaskCompleted, `{"task_id":"t1","type":"receipt","status":"succeeded"}`)
	if rec := send(http.MethodPost, ok, Sign(secret, ok)); rec.Code != http.StatusOK || got == nil || got.Type != prro.EventTaskCompleted {
		t.Errorf("valid delivery: status %d, event %+v", rec.Code, got)
	}

	failed := envelope(prro.EventTaskFailed, `{}`)
	malformed := []byte(`{"event":`)
	huge := make([]byte, MaxBodyBytes+1)
	tests := []struct {
		name   string
		method string
		body   []byte
		sig    string
		want   int
	}{
		{"handler error", http.MethodPost, failed, Sign(secret, failed), http.StatusInternalServerError},
		{"bad signature", http.MethodPost, ok, Sign("other", ok), http.StatusUnauthorized},
		{"no signature", http.MethodPost, ok, "", http.StatusUnauthorized},
		{"malformed", http.MethodPost, malformed, Sign(secret, malformed), http.StatusBadRequest},
		{"too large", http.MethodPost, huge, Sign(secret, huge), http.StatusRequestEntityTooLarge},
		{"GET", http.MethodGet, nil, "", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := send(tt.method, tt.body, tt.sig); rec.Code != tt.want {
				t.Errorf("status = %d (%s), want %d", rec.Code, rec.Body, tt.want)
			}
		})
	}
}

func FuzzVerify(f *testing.F) {
	f.Add([]byte(`{"event":"task.completed"}`), "sha256=00")
	f.Add([]byte{}, "")
	f.Fuzz(func(t *testing.T, body []byte, sig string) {
		if err := Verify(secret, body, Sign(secret, body)); err != nil {
			t.Fatalf("own signature rejected: %v", err)
		}
		if sig != Sign(secret, body) && !strings.EqualFold(sig, Sign(secret, body)) {
			if err := Verify(secret, body, sig); err == nil {
				t.Fatalf("forged signature %q accepted", sig)
			}
		}
	})
}
