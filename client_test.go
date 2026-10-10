package prro

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient піднімає httptest-сервер з handler і клієнт до нього з
// мілісекундними паузами між повторами.
func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient("test-token", append([]Option{WithBaseURL(srv.URL + "/")}, opts...)...)
	c.retryMin, c.retryMax = time.Millisecond, 2*time.Millisecond
	c.pollMin, c.pollMax = time.Millisecond, 2*time.Millisecond
	return c
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

var uuidV7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestRequestHeaders(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/whoami" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "my-shop/1.0 prro-go/"+Version) {
			t.Errorf("User-Agent = %q", got)
		}
		if got := r.Header.Get("Accept-Language"); got != "en" {
			t.Errorf("Accept-Language = %q", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "" {
			t.Errorf("GET carries Idempotency-Key %q", got)
		}
		writeJSON(w, 200, `{"client_id":"c1","scopes":["fiscal"],"all_cash_registers":true,"jti":"j1"}`)
	}, WithUserAgent("my-shop/1.0"), WithLanguage("en"))

	me, err := c.System.WhoAmI(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.ClientID != "c1" || me.TokenID != "j1" || !me.AllCashRegisters || !me.ExpiresAt.IsZero() {
		t.Errorf("identity = %+v", me)
	}
}

func TestIdentityExpiresAt(t *testing.T) {
	want := time.Date(2026, 10, 7, 18, 20, 50, 0, time.UTC)
	tests := map[string]time.Time{
		`"2026-10-07T18:20:50Z"`: want,
		`1791397250`:             want, // числом, як віддає сервер 0.5.0
		`null`:                   {},
	}
	for raw, exp := range tests {
		var id Identity
		err := json.Unmarshal([]byte(`{"client_id":"c1","jti":"j1","expires_at":`+raw+`}`), &id)
		if err != nil || !id.ExpiresAt.Equal(exp) || id.ClientID != "c1" || id.TokenID != "j1" {
			t.Errorf("expires_at %s: got %v, %+v, err %v", raw, id.ExpiresAt, id, err)
		}
	}
	var id Identity
	if err := json.Unmarshal([]byte(`{"client_id":"c1"}`), &id); err != nil || !id.ExpiresAt.IsZero() {
		t.Errorf("absent expires_at: %v, %v", id.ExpiresAt, err)
	}
	if err := json.Unmarshal([]byte(`{"expires_at":true}`), &id); err == nil {
		t.Error("expires_at=true: no error")
	}
}

func TestFiscalRequestGeneratesStableKey(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var keys []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		mu.Unlock()
		if got := r.URL.Query().Get("mode"); got != "async" {
			t.Errorf("mode = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"cashier":"Каса 1"}` {
			t.Errorf("body = %s", body)
		}
		if calls.Add(1) == 1 {
			writeJSON(w, 503, `{"code":"unavailable","message":"busy"}`)
			return
		}
		writeJSON(w, 202, `{"task_id":"t1"}`)
	})

	var out struct {
		TaskID string `json:"task_id"`
	}
	err := c.do(context.Background(), request{
		method: http.MethodPost, path: "/v1/cash-registers/r1/shifts",
		body: map[string]string{"cashier": "Каса 1"}, fiscal: true,
	}, &out, WithAsync())
	if err != nil {
		t.Fatal(err)
	}
	if out.TaskID != "t1" || calls.Load() != 2 {
		t.Fatalf("task = %q, calls = %d", out.TaskID, calls.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if !uuidV7.MatchString(keys[0]) || keys[0] != keys[1] {
		t.Errorf("keys = %q, want one UUIDv7 reused on retry", keys)
	}
}

func TestFiscalRequestExplicitKey(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Idempotency-Key"); got != "order-10412" {
			t.Errorf("Idempotency-Key = %q", got)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want sync mode by default", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	err := c.do(context.Background(), request{method: http.MethodDelete, path: "/v1/x", fiscal: true},
		nil, WithIdempotencyKey("order-10412"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoRetryForUnsafeRequest(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeJSON(w, 503, `{"code":"unavailable","message":"busy"}`)
	})
	err := c.do(context.Background(), request{method: http.MethodPost, path: "/v1/webhooks"}, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestRetryExhausted(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "<html>bad gateway</html>")
	}, WithRetry(3))
	_, err := c.System.Version(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadGateway || apiErr.Code != "" {
		t.Fatalf("err = %v", err)
	}
	if apiErr.Message != "<html>bad gateway</html>" || errors.Unwrap(apiErr) != nil {
		t.Errorf("message = %q, unwrap = %v", apiErr.Message, errors.Unwrap(apiErr))
	}
	if calls.Load() != 4 {
		t.Errorf("calls = %d, want 1 + 3 retries", calls.Load())
	}
}

func TestRetryOnNetworkError(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		writeJSON(w, 200, `{"version":"v0.3.0"}`)
	})
	v, err := c.System.Version(context.Background())
	if err != nil || v != "v0.3.0" {
		t.Fatalf("version = %q, err = %v", v, err)
	}
}

func TestContextCanceledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c.retryMin, c.retryMax = time.Hour, time.Hour

	done := make(chan error, 1)
	go func() { _, err := c.System.Version(ctx); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("backoff ignored canceled context")
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"unauthorized", 401, `{"code":"unauthorized","message":"token revoked"}`, ErrUnauthorized},
		{"forbidden", 403, `{"code":"forbidden","message":"scope"}`, ErrForbidden},
		{"not found", 404, `{"code":"not_found","message":"cash register not found"}`, ErrNotFound},
		{"payment", 402, `{"code":"payment_required","message":"balance"}`, ErrPaymentRequired},
		{"key reused", 409, `{"code":"idempotency_key_reused","message":"other op"}`, ErrIdempotencyKeyReused},
		{"webhook limit", 409, `{"code":"webhook_limit_reached","message":"limit"}`, ErrWebhookLimitReached},
		{"validation", 422, `{"code":"validation_failed","message":"items","details":[{"field":"items"}]}`, ErrValidation},
		{"status fallback", 404, `not found`, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, tt.status, tt.body)
			})
			_, err := c.System.WhoAmI(context.Background())
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestTaskFailedError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 422, `{"code":"task_failed","message":"no open shift","details":{
			"task_id":"t1","fault":{"code":"state.shift_not_open","class":"precursor","message":"Зміну не відкрито"}}}`)
	})
	err := c.do(context.Background(), request{method: http.MethodPost, path: "/v1/receipts", fiscal: true}, nil)
	var apiErr *Error
	if !errors.Is(err, ErrTaskFailed) || !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if apiErr.TaskID != "t1" || apiErr.Fault == nil || apiErr.Fault.Class != FaultPrecursor {
		t.Fatalf("error = %+v, fault = %+v", apiErr, apiErr.Fault)
	}
	if want := "prro: 422 task_failed (state.shift_not_open): no open shift"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestDPS(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"effective":"online","verdict":"degraded","since":"2026-07-14T10:15:04Z",
			"latency_ms":1800,"samples":40,"failures":1,
			"tunables":{"window_seconds":300,"degraded_latency_ms":1500,"probe_interval_seconds":15}}`)
	})
	s, err := c.System.DPS(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Effective != DPSOnline || s.Verdict != DPSDegraded || s.Tunables.DegradedLatencyMS != 1500 {
		t.Errorf("status = %+v", s)
	}
	if !s.Since.Equal(time.Date(2026, 7, 14, 10, 15, 4, 0, time.UTC)) {
		t.Errorf("since = %v", s.Since)
	}
}

func TestBackoff(t *testing.T) {
	c := NewClient("")
	for attempt := range 10 {
		d := c.backoff(attempt, nil)
		ceil := min(defaultRetryMin<<attempt, defaultRetryMax)
		if d < ceil/2 || d > ceil {
			t.Errorf("attempt %d: backoff %v outside [%v, %v]", attempt, d, ceil/2, ceil)
		}
	}
	resp := &http.Response{Header: http.Header{"Retry-After": {"3"}}}
	if d := c.backoff(0, resp); d != 3*time.Second {
		t.Errorf("Retry-After backoff = %v", d)
	}
	resp.Header.Set("Retry-After", "120")
	if d := c.backoff(0, resp); d != defaultRetryMax {
		t.Errorf("Retry-After not capped: %v", d)
	}
}

func TestNewIdempotencyKey(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		k := newIdempotencyKey()
		if !uuidV7.MatchString(k) || seen[k] {
			t.Fatalf("bad or duplicate key %q", k)
		}
		seen[k] = true
	}
}

func TestOptions(t *testing.T) {
	hc := &http.Client{}
	c := NewClient("tok", WithHTTPClient(hc), WithHTTPClient(nil), WithRetry(-1), WithUserAgent("  "), WithBaseURL(""))
	if c.httpClient != hc || c.maxRetries != 0 || c.userAgent != defaultUserAgent || c.baseURL != DefaultBaseURL {
		t.Errorf("client = %+v", c)
	}
}

func TestDecodeError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"version":`)
	})
	_, err := c.System.Version(context.Background())
	var syntaxErr *json.SyntaxError
	if err == nil || errors.As(err, new(*Error)) || (!errors.As(err, &syntaxErr) && !errors.Is(err, io.ErrUnexpectedEOF)) {
		t.Fatalf("err = %v", err)
	}
}
