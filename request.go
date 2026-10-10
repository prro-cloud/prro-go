package prro

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"uuid"
)

// request описує один виклик API.
type request struct {
	method string
	path   string
	query  url.Values
	body   any
	// fiscal — операція вимагає Idempotency-Key і приймає ?mode=.
	fiscal bool
}

var errEmptyID = errors.New("prro: empty id")

// pathf підставляє в format екрановані ідентифікатори; порожній
// ідентифікатор — помилка, а не запит до сусіднього маршруту.
func pathf(format string, ids ...string) (string, error) {
	args := make([]any, len(ids))
	for i, id := range ids {
		if id == "" {
			return "", errEmptyID
		}
		args[i] = url.PathEscape(id)
	}
	return fmt.Sprintf(format, args...), nil
}

// call виконує запит і повертає декодовану відповідь типу T.
func call[T any](ctx context.Context, c *Client, r request, opts ...CallOption) (*T, error) {
	out := new(T)
	if err := c.do(ctx, r, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

// do виконує запит із повторами і декодує успішну відповідь в out
// (nil — тіло відповіді відкидається).
func (c *Client) do(ctx context.Context, r request, out any, opts ...CallOption) error {
	var co callOptions
	for _, opt := range opts {
		opt(&co)
	}

	var payload []byte
	if r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil {
			return fmt.Errorf("prro: encode request: %w", err)
		}
		payload = b
	}

	query := url.Values{}
	maps.Copy(query, r.query)
	var key string
	if r.fiscal {
		key = co.idempotencyKey
		if key == "" {
			key = newIdempotencyKey()
		}
		if co.async {
			query.Set("mode", "async")
		}
	}
	endpoint := c.baseURL + r.path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	// Повторювати безпечно лише те, що не може виконатися двічі:
	// GET і PUT ідемпотентні за семантикою HTTP, фіскальні операції — за
	// ключем.
	retryable := r.method == http.MethodGet || r.method == http.MethodPut || key != ""

	for attempt := 0; ; attempt++ {
		req, err := c.newRequest(ctx, r.method, endpoint, payload, key)
		if err != nil {
			return fmt.Errorf("prro: %s %s: %w", r.method, r.path, err)
		}
		start := time.Now()
		resp, err := c.httpClient.Do(req)
		// Скасований ctx тут не перевіряється: його ловить sleep перед
		// паузою, тож виклик завершується context.Canceled незалежно від
		// того, що транспорт встиг повернути — відповідь чи помилку.
		canRetry := retryable && attempt < c.maxRetries

		if err != nil {
			c.log(ctx, "prro: request failed", r, attempt, 0, start, err)
			if !canRetry {
				return fmt.Errorf("prro: %s %s: %w", r.method, r.path, err)
			}
			if err := sleep(ctx, c.backoff(attempt, nil)); err != nil {
				return fmt.Errorf("prro: %s %s: %w", r.method, r.path, err)
			}
			continue
		}
		c.log(ctx, "prro: request", r, attempt, resp.StatusCode, start, nil)

		if canRetry && retryableStatus(resp.StatusCode) {
			wait := c.backoff(attempt, resp)
			drain(resp)
			if err := sleep(ctx, wait); err != nil {
				return fmt.Errorf("prro: %s %s: %w", r.method, r.path, err)
			}
			continue
		}
		return decodeResponse(resp, out)
	}
}

func (c *Client) newRequest(ctx context.Context, method, endpoint string, payload []byte, key string) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if c.language != "" {
		req.Header.Set("Accept-Language", c.language)
	}
	return req, nil
}

func decodeResponse(resp *http.Response, out any) error {
	defer drain(resp)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return parseError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("prro: decode response: %w", err)
	}
	return nil
}

// drain дочитує і закриває тіло, щоб з'єднання повернулося в пул.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// backoff — пауза перед повтором attempt: Retry-After, якщо сервер його
// надіслав, інакше експонента з випадковим розкидом у межах [d/2, d].
func (c *Client) backoff(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
			return min(d, c.retryMax)
		}
	}
	d := c.retryMin << attempt
	if d > c.retryMax || d <= 0 {
		d = c.retryMax
	}
	return d/2 + mrand.N(d/2+1)
}

func parseRetryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0), true
	}
	return 0, false
}

// sleep чекає d або скасування ctx. Уже скасований ctx — одразу помилка:
// select обирав би випадково, якби таймер теж був готовий.
func sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) log(ctx context.Context, msg string, r request, attempt, status int, start time.Time, err error) {
	if c.logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("method", r.method),
		slog.String("path", r.path),
		slog.Int("attempt", attempt+1),
		slog.Duration("duration", time.Since(start)),
	}
	if status != 0 {
		attrs = append(attrs, slog.Int("status", status))
	}
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}
	c.logger.LogAttrs(ctx, slog.LevelDebug, msg, attrs...)
}

// newIdempotencyKey повертає UUIDv7 (RFC 9562): ключі впорядковані за
// часом і не повторюються.
func newIdempotencyKey() string { return uuid.NewV7().String() }
