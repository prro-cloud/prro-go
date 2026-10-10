package prro

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Помилки для перевірки через [errors.Is]. Їх обгортає [*Error] за машинним
// кодом відповіді, а якщо коду немає — за HTTP-статусом.
var (
	// ErrUnauthorized — 401: токена немає, він недійсний, протермінований чи
	// відкликаний.
	ErrUnauthorized = errors.New("prro: unauthorized")
	// ErrForbidden — 403: обмежений токен звернувся до ресурсу рівня
	// клієнта (білінг, вебхуки).
	ErrForbidden = errors.New("prro: forbidden")
	// ErrNotFound — 404: ресурсу немає або токен не має до нього доступу.
	ErrNotFound = errors.New("prro: not found")
	// ErrPaymentRequired — 402: баланс нижче порога, нові чеки заблоковано
	// до поповнення. Операції зі змінами лишаються доступними.
	ErrPaymentRequired = errors.New("prro: payment required")
	// ErrIdempotencyKeyReused — 409: цей Idempotency-Key уже використано
	// для іншої операції.
	ErrIdempotencyKeyReused = errors.New("prro: idempotency key reused")
	// ErrWebhookLimitReached — 409: ліміт вебхуків вичерпано (не більше, ніж
	// кас у клієнта, плюс один); limit і used — у [Error.Details].
	ErrWebhookLimitReached = errors.New("prro: webhook limit reached")
	// ErrValidation — 422: запит не пройшов валідацію; подробиці — у
	// [Error.Details].
	ErrValidation = errors.New("prro: validation failed")
	// ErrTaskFailed — 422: фіскальне завдання завершилося помилкою (немає
	// відкритої зміни, відмова ДПС тощо); опис збою — у [Error.Fault].
	ErrTaskFailed = errors.New("prro: task failed")
)

// Машинні коди помилок API, яким відповідають помилки вище.
const (
	codeUnauthorized         = "unauthorized"
	codeForbidden            = "forbidden"
	codeNotFound             = "not_found"
	codePaymentRequired      = "payment_required"
	codeIdempotencyKeyReused = "idempotency_key_reused"
	codeWebhookLimitReached  = "webhook_limit_reached"
	codeValidationFailed     = "validation_failed"
	codeTaskFailed           = "task_failed"
)

// Межі тіла відповіді, яке не є конвертом API (сторінка помилки проксі).
const (
	maxErrorMessageBytes      = 512
	maxErrorResponseBodyBytes = 64 << 10
)

// Error — відповідь API з помилкою: конверт {code, message, details}.
type Error struct {
	// StatusCode — HTTP-статус відповіді.
	StatusCode int
	// Code — стабільний машинний код помилки ("validation_failed",
	// "task_failed", …); саме на нього реагує інтеграція. Порожній, якщо
	// відповідь прийшла не від API (наприклад, сторінка помилки проксі).
	Code string
	// Message — пояснення для людини; покладатися на його текст
	// програмно не можна.
	Message string
	// Details — подробиці помилки як є, наприклад перелік полів, які не
	// пройшли валідацію.
	Details json.RawMessage
	// TaskID — завдання, що завершилося помилкою; лише для [ErrTaskFailed].
	TaskID string
	// Fault — структурований опис збою завдання; лише для [ErrTaskFailed].
	Fault *Fault
}

// Error повертає опис помилки: статус, код і повідомлення.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("prro: ")
	b.WriteString(strconv.Itoa(e.StatusCode))
	if e.Code != "" {
		b.WriteString(" " + e.Code)
	}
	if e.Fault != nil && e.Fault.Code != "" {
		b.WriteString(" (" + e.Fault.Code + ")")
	}
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	return b.String()
}

// Unwrap повертає відповідну помилку-маркер (ErrNotFound, ErrTaskFailed, …)
// або nil, якщо такої немає.
func (e *Error) Unwrap() error {
	switch e.Code {
	case codeUnauthorized:
		return ErrUnauthorized
	case codeForbidden:
		return ErrForbidden
	case codeNotFound:
		return ErrNotFound
	case codePaymentRequired:
		return ErrPaymentRequired
	case codeIdempotencyKeyReused:
		return ErrIdempotencyKeyReused
	case codeWebhookLimitReached:
		return ErrWebhookLimitReached
	case codeValidationFailed:
		return ErrValidation
	case codeTaskFailed:
		return ErrTaskFailed
	}
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusPaymentRequired:
		return ErrPaymentRequired
	}
	return nil
}

// FaultClass — що може зарадити збою завдання.
type FaultClass string

// Класи збою завдання.
const (
	// FaultResync — звірити стан із ДПС.
	FaultResync FaultClass = "resync"
	// FaultConfig — виправити налаштування реєстратора.
	FaultConfig FaultClass = "config"
	// FaultPrecursor — виконати пропущений попередній крок.
	FaultPrecursor FaultClass = "precursor"
	// FaultUserAction — потрібне рішення людини.
	FaultUserAction FaultClass = "user_action"
	// FaultTransient — тимчасова недоступність, допоможе повтор.
	FaultTransient FaultClass = "transient"
	// FaultRequest — виправити запит.
	FaultRequest FaultClass = "request"
	// FaultInternal — збій сервісу.
	FaultInternal FaultClass = "internal"
)

// Fault — структурований опис збою завдання. Простори імен коду: dps.* —
// відмова фіскального сервера, fiscal.* — валідація документа, return.* —
// звірка повернення з початковим чеком, signer.* — рівень КЕП, state.* —
// стан реєстратора.
type Fault struct {
	// Code — стабільний ідентифікатор, напр. "dps.check_local_number_invalid".
	Code string `json:"code"`
	// Class — що може зарадити.
	Class FaultClass `json:"class"`
	// Params — значення для підстановки в Message.
	Params map[string]any `json:"params,omitempty"`
	// Message — текст для людини мовою з [WithLanguage].
	Message string `json:"message,omitempty"`
	// UpstreamMessage — дослівне повідомлення фіскального сервера.
	UpstreamMessage string `json:"upstream_message,omitempty"`
	// Remediation — автоматичні виправлення, застосовані під час обробки
	// завдання: "local_number_synced", "shift_adopted", …
	Remediation []string `json:"remediation,omitempty"`
}

// parseError будує [*Error] з відповіді не-2xx. Тіло, яке не є конвертом
// API, потрапляє в Message (обрізане), а Code лишається порожнім.
func parseError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorResponseBodyBytes))
	e := &Error{StatusCode: resp.StatusCode}

	var env struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Details json.RawMessage `json:"details"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Code != "" {
		e.Code, e.Message = env.Code, env.Message
		if len(env.Details) > 0 && string(env.Details) != "null" {
			e.Details = env.Details
		}
		if e.Code == codeTaskFailed && e.Details != nil {
			var d struct {
				TaskID string `json:"task_id"`
				Fault  *Fault `json:"fault"`
			}
			if json.Unmarshal(e.Details, &d) == nil {
				e.TaskID, e.Fault = d.TaskID, d.Fault
			}
		}
		return e
	}

	msg := strings.TrimSpace(string(raw))
	if len(msg) > maxErrorMessageBytes {
		msg = strings.ToValidUTF8(msg[:maxErrorMessageBytes], "") + "…"
	}
	if msg == "" {
		msg = strings.ToLower(http.StatusText(resp.StatusCode))
	}
	e.Message = msg
	return e
}
