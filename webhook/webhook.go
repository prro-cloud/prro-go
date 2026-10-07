// Package webhook — приймач вебхуків PRRO.cloud: перевірка підпису,
// розбір конверта доставки і типізовані дані подій.
//
// Сервіс надсилає кожну подію POST-запитом із тілом {delivery_id, event,
// attempt, sequence, occurred_at, data} і заголовком X-Signature: "sha256=<hex>" —
// HMAC-SHA256 тіла на секреті вебхука.
//
// # Секрет
//
// Секрет — рядок, яким сервіс підписує доставки, а приймач перевіряє
// підпис. Його видають рівно один раз, коли вебхук створюють: у кабінеті
// (розділ «Webhooks» → «Додати webhook») або через
// [prro.WebhooksService.Create] — поле [prro.CreatedWebhook.Secret].
// Повторно отримати секрет неможливо: збережіть його одразу, наприклад у
// змінну оточення.
//
// # Приймач
//
// Найпростіший приймач — [Handler]:
//
//	secret := os.Getenv("PRRO_WEBHOOK_SECRET")
//	http.Handle("/prro-hook", webhook.Handler(secret, func(ctx context.Context, e *webhook.Event) error {
//		switch e.Type {
//		case prro.EventTaskCompleted, prro.EventTaskFailed:
//			d, err := e.Task()
//			if err != nil {
//				return err
//			}
//			return markOrder(ctx, d.TaskID, d.Status)
//		}
//		return nil
//	}))
//
// Доставка гарантована «щонайменше один раз»: та сама подія може прийти
// повторно з тим самим [Event.DeliveryID] — відкидайте повтори за ним.
//
// Події однієї каси надсилаються по одній і по порядку, але подія, яку
// приймач не прийняв, не затримує наступні: після повтору чи replay вона
// може прийти пізніше за них. Упорядковуйте події каси за [Event.Sequence]
// (зростає, пропуски можливі); актуальний стан каси —
// [prro.CashRegistersService.Get].
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prro-cloud/prro-go"
)

// SignatureHeader — заголовок із підписом доставки.
const SignatureHeader = "X-Signature"

// MaxBodyBytes — найбільше тіло доставки, яке читає [ParseRequest].
// Подія register.offline_abandoned перелічує всі документи сесії, тож межа
// має запас.
const MaxBodyBytes = 10 << 20

// ErrInvalidSignature — підпис відсутній, має не той формат або не
// збігається з тілом: доставку надіслав не PRRO.cloud або тіло змінено.
var ErrInvalidSignature = errors.New("webhook: invalid signature")

const signaturePrefix = "sha256="

// Sign повертає значення заголовка X-Signature для тіла body — так його
// рахує сервіс. Знадобиться для тестів власного приймача.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// Verify перевіряє підпис signature тіла body за сталий час. Перевіряйте
// підпис до будь-якої обробки тіла.
func Verify(secret string, body []byte, signature string) error {
	got, ok := strings.CutPrefix(signature, signaturePrefix)
	if !ok {
		return ErrInvalidSignature
	}
	sum, err := hex.DecodeString(got)
	if err != nil {
		return ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(sum, mac.Sum(nil)) {
		return ErrInvalidSignature
	}
	return nil
}

// Event — конверт доставки події.
type Event struct {
	// DeliveryID — ідентифікатор доставки; однаковий в усіх повторах, тож
	// за ним відкидають дублікати.
	DeliveryID string `json:"delivery_id"`
	// Type — тип події.
	Type prro.WebhookEvent `json:"event"`
	// Attempt — номер спроби доставки, від 1.
	Attempt int `json:"attempt"`
	// Sequence — номер події в межах каси: зростає, пропуски можливі; за ним
	// упорядковують події каси. 0 у подій рівня клієнта.
	Sequence int64 `json:"sequence,omitempty"`
	// OccurredAt — коли подія сталася.
	OccurredAt time.Time `json:"occurred_at"`
	// Data — корисне навантаження; типізовано його повертають методи
	// [Event.Task], [Event.Shift] тощо.
	Data json.RawMessage `json:"data"`
}

// Parse перевіряє підпис signature і розбирає тіло доставки body.
func Parse(secret string, body []byte, signature string) (*Event, error) {
	if err := Verify(secret, body, signature); err != nil {
		return nil, err
	}
	e := new(Event)
	if err := json.Unmarshal(body, e); err != nil {
		return nil, fmt.Errorf("webhook: decode envelope: %w", err)
	}
	return e, nil
}

// ParseRequest читає тіло запиту (не більше [MaxBodyBytes]), перевіряє
// підпис із заголовка [SignatureHeader] і розбирає конверт.
func ParseRequest(r *http.Request, secret string) (*Event, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("webhook: read body: %w", err)
	}
	if len(body) > MaxBodyBytes {
		return nil, errBodyTooLarge
	}
	return Parse(secret, body, r.Header.Get(SignatureHeader))
}

var errBodyTooLarge = fmt.Errorf("webhook: body exceeds %d bytes", MaxBodyBytes)
