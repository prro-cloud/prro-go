package prro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// WebhookEvent — тип події вебхука.
type WebhookEvent string

// Події, на які можна підписатися.
const (
	// EventTaskCompleted — завдання завершилося успішно.
	EventTaskCompleted WebhookEvent = "task.completed"
	// EventTaskFailed — завдання завершилося помилкою.
	EventTaskFailed WebhookEvent = "task.failed"
	// EventShiftOpened — зміну відкрито.
	EventShiftOpened WebhookEvent = "shift.opened"
	// EventShiftClosed — зміну закрито.
	EventShiftClosed WebhookEvent = "shift.closed"
	// EventKeyExpiring — сертифікат ключа КЕП спливає протягом 30 днів;
	// раз на добу.
	EventKeyExpiring WebhookEvent = "key.expiring"
	// EventRegisterOffline — каса перейшла в офлайн-сесію.
	EventRegisterOffline WebhookEvent = "register.offline"
	// EventRegisterOnline — каса повернулася онлайн.
	EventRegisterOnline WebhookEvent = "register.online"
	// EventRegisterOfflineLimit — наближення до законних меж офлайн-сесії
	// (36 і 168 годин).
	EventRegisterOfflineLimit WebhookEvent = "register.offline_limit"
	// EventRegisterOfflineAbandoned — від офлайн-сесії відмовлено; у даних —
	// документи, яких ДПС не отримала.
	EventRegisterOfflineAbandoned WebhookEvent = "register.offline_abandoned"
	// EventRegisterRemediated — сервіс сам усунув розбіжність із ДПС.
	EventRegisterRemediated WebhookEvent = "register.remediated"
	// EventRegisterNeedsAttention — розбіжності не усуваються самі, на касу
	// має поглянути людина.
	EventRegisterNeedsAttention WebhookEvent = "register.needs_attention"
	// EventClientBalanceLow — баланс опустився нижче порога; попередження
	// перед тим, як чеки почнуть відхилятися з 402.
	EventClientBalanceLow WebhookEvent = "client.balance_low"
	// EventReceiptRegistered — зареєстровано чек (продаж, повернення, сторно,
	// службове внесення чи видача) з повним вмістом: позиції, оплати,
	// податки — у тому вигляді, в якому його отримала ДПС.
	EventReceiptRegistered WebhookEvent = "receipt.registered"
)

// Зарезервовані події: на них не підписуються, але доставка може їх нести.
const (
	// EventPing — тестова відправка з кабінету.
	EventPing WebhookEvent = "webhook.ping"
	// EventDPSOffline — сервіс перейшов в офлайн-режим роботи з ДПС.
	EventDPSOffline WebhookEvent = "system.dps_offline"
	// EventDPSOnline — сервіс повернувся в онлайн-режим роботи з ДПС.
	EventDPSOnline WebhookEvent = "system.dps_online"
	// EventDPSFlapping — режим роботи з ДПС часто перемикається.
	EventDPSFlapping WebhookEvent = "system.dps_flapping"
	// EventDPSModeChanged — режим роботи з ДПС задано вручну.
	EventDPSModeChanged WebhookEvent = "system.dps_mode_changed"
)

// WebhookEndpoint — точка доставки подій: куди слати й на які саме.
type WebhookEndpoint struct {
	ID string `json:"id"`
	// URL — абсолютний http(s)-URL приймача.
	URL string `json:"url"`
	// Events — події підписки; порожній перелік — усі події.
	Events []WebhookEvent `json:"events"`
	// Active — доставка ввімкнена; false призупиняє її, не втрачаючи черги.
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreatedWebhook — щойно створена точка доставки разом із секретом.
type CreatedWebhook struct {
	WebhookEndpoint
	// Secret — секрет HMAC для перевірки підпису доставок. Повертається
	// рівно один раз — збережіть його одразу.
	Secret string `json:"secret"`
}

// DeliveryStatus — стан доставки події.
type DeliveryStatus string

// Стани доставки.
const (
	DeliveryPending    DeliveryStatus = "pending"    // у черзі
	DeliveryDelivering DeliveryStatus = "delivering" // триває спроба
	DeliveryDelivered  DeliveryStatus = "delivered"  // доставлено
	DeliveryFailed     DeliveryStatus = "failed"     // спроба невдала, буде повтор
	DeliveryDead       DeliveryStatus = "dead"       // спроби вичерпано
)

// WebhookDelivery — одна доставка події та її стан.
type WebhookDelivery struct {
	// ID — ідентифікатор доставки; його передають у
	// [WebhooksService.Replay].
	ID string `json:"id"`
	// Event — тип події; крім підписних, буває webhook.ping і system.dps_*.
	Event    WebhookEvent   `json:"event"`
	Status   DeliveryStatus `json:"status"`
	Attempts int            `json:"attempts"`
	// CashRegisterID — каса, якої стосується подія; порожній у подій рівня
	// клієнта.
	CashRegisterID string `json:"cash_register_id,omitempty"`
	// Sequence — номер події в межах каси (поле sequence конверта); за ним
	// упорядковують події. 0 у подій рівня клієнта.
	Sequence int64 `json:"sequence,omitempty"`
	// Data — корисне навантаження події.
	Data json.RawMessage `json:"data"`
	// LastError — чим завершилася остання невдала спроба.
	LastError     string    `json:"last_error,omitempty"`
	NextAttemptAt time.Time `json:"next_attempt_at,omitzero"`
	CreatedAt     time.Time `json:"created_at"`
	DeliveredAt   time.Time `json:"delivered_at,omitzero"`
}

// WebhooksService — реєстрація вебхуків і історія доставок.
type WebhooksService service

// WebhookList — вебхуки клієнта і ліміт їхньої кількості.
type WebhookList struct {
	// Webhooks — точки доставки; секрети не повертаються.
	Webhooks []WebhookEndpoint `json:"webhooks"`
	// Limit — скільки вебхуків дозволено: кількість кас клієнта плюс один.
	Limit int `json:"limit"`
	// Used — скільки вебхуків зареєстровано, включно з призупиненими.
	Used int `json:"used"`
}

// List повертає вебхуки клієнта разом із лімітом: поки Used < Limit, можна
// створити ще один.
func (s *WebhooksService) List(ctx context.Context) (*WebhookList, error) {
	return call[WebhookList](ctx, s.client, request{method: http.MethodGet, path: "/v1/webhooks"})
}

// CreateWebhookParams — параметри нового вебхука.
type CreateWebhookParams struct {
	// URL — абсолютний http(s)-URL приймача; обов'язковий.
	URL string `json:"url"`
	// Events — події підписки; порожній перелік — усі події.
	Events []WebhookEvent `json:"events,omitempty"`
	// Secret — секрет HMAC; порожній — згенерує сервер.
	Secret string `json:"secret,omitempty"`
}

// Create реєструє вебхук. Доставка — POST із конвертом {delivery_id, event,
// attempt, occurred_at, data} і підписом у заголовку X-Signature; гарантія —
// «щонайменше один раз», тож приймач відкидає повтори за delivery_id.
// Секрет у відповіді повертається рівно один раз.
//
// Вебхуків може бути не більше, ніж кас у клієнта, плюс один; призупинені
// теж рахуються. Понад ліміт — [ErrWebhookLimitReached], а поточні limit і
// used — у [Error.Details].
func (s *WebhooksService) Create(ctx context.Context, params CreateWebhookParams) (*CreatedWebhook, error) {
	return call[CreatedWebhook](ctx, s.client, request{method: http.MethodPost, path: "/v1/webhooks", body: params})
}

// UpdateWebhookParams — зміни вебхука; незаповнені поля не змінюються.
type UpdateWebhookParams struct {
	// URL — новий абсолютний http(s)-URL приймача.
	URL string `json:"url,omitempty"`
	// Events — новий перелік подій. nil — без змін; порожній, але не nil
	// зріз []WebhookEvent{} — підписка на всі події.
	Events []WebhookEvent `json:"events,omitzero"`
	// Active — false призупиняє доставку, не втрачаючи черги; true
	// відновлює. nil — без змін.
	Active *bool `json:"active,omitempty"`
}

// Update змінює URL, підписку або активність вебхука id. На час ремонту
// свого приймача вимикайте вебхук (Active: new(false)), а не
// видаляйте: видалення переводить недоставлене в dead.
func (s *WebhooksService) Update(ctx context.Context, id string, params UpdateWebhookParams) (*WebhookEndpoint, error) {
	path, err := pathf("/v1/webhooks/%s", id)
	if err != nil {
		return nil, err
	}
	return call[WebhookEndpoint](ctx, s.client, request{method: http.MethodPatch, path: path, body: params})
}

// Delete видаляє вебхук id; недоставлені події переходять у dead.
func (s *WebhooksService) Delete(ctx context.Context, id string) error {
	path, err := pathf("/v1/webhooks/%s", id)
	if err != nil {
		return err
	}
	return s.client.do(ctx, request{method: http.MethodDelete, path: path}, nil)
}

// ListDeliveriesParams — фільтр історії доставок.
type ListDeliveriesParams struct {
	// Status — лише доставки в цьому стані; порожній — усі.
	Status DeliveryStatus
	// Limit — скільки доставок повернути: 1–200; 0 — типово 50.
	Limit int
}

// Deliveries повертає історію доставок вебхука id, від найновішої; params
// може бути nil. Доставлені зберігаються 30 днів, невдалі (dead) — 14 днів
// від останньої спроби; старіші зникають з історії.
func (s *WebhooksService) Deliveries(ctx context.Context, id string, params *ListDeliveriesParams) ([]WebhookDelivery, error) {
	path, err := pathf("/v1/webhooks/%s/deliveries", id)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if params != nil {
		if params.Status != "" {
			q.Set("status", string(params.Status))
		}
		if params.Limit > 0 {
			q.Set("limit", strconv.Itoa(params.Limit))
		}
	}
	out, err := call[struct {
		Deliveries []WebhookDelivery `json:"deliveries"`
	}](ctx, s.client, request{method: http.MethodGet, path: path, query: q})
	if err != nil {
		return nil, err
	}
	return out.Deliveries, nil
}

// Replay повертає доставку deliveryID вебхука id у чергу з новим запасом
// спроб; спроба — одразу. Доступно, поки доставка є в історії: невдала —
// 14 днів від останньої спроби, далі — [ErrNotFound]. Події, доставлені тим
// часом, не повторюються: ця прийде після них, тож упорядковуйте події за
// Sequence. Якщо саме зараз триває спроба доставки — 409
// (Code == "delivery_in_flight").
func (s *WebhooksService) Replay(ctx context.Context, id, deliveryID string) (*WebhookDelivery, error) {
	path, err := pathf("/v1/webhooks/%s/deliveries/%s/replay", id, deliveryID)
	if err != nil {
		return nil, err
	}
	return call[WebhookDelivery](ctx, s.client, request{method: http.MethodPost, path: path})
}
