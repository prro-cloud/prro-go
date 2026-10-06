package prro

import (
	"log/slog"
	"net/http"
	"strings"
)

// Option налаштовує [Client] під час створення в [NewClient].
type Option func(*Client)

// WithBaseURL задає базову адресу API замість [DefaultBaseURL] — наприклад
// для власного проксі або локального сервера. Порожній рядок залишає
// адресу за замовчуванням, тож значення можна брати прямо зі змінної
// оточення: WithBaseURL(os.Getenv("PRRO_BASE_URL")).
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL = strings.TrimRight(baseURL, "/"); baseURL != "" {
			c.baseURL = baseURL
		}
	}
}

// WithHTTPClient задає HTTP-клієнт для запитів: власний транспорт, проксі,
// тайм-аути. Тайм-аут має перевищувати 30 секунд синхронного очікування
// ДПС. nil залишає клієнт за замовчуванням.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithUserAgent додає назву вашого застосунку на початок заголовка
// User-Agent: "my-shop/1.2 prro-go/0.1.0 (…)".
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua = strings.TrimSpace(ua); ua != "" {
			c.userAgent = ua + " " + defaultUserAgent
		}
	}
}

// WithLanguage задає мову повідомлень про помилки (заголовок
// Accept-Language): "uk" (типово) або "en".
func WithLanguage(lang string) Option {
	return func(c *Client) { c.language = lang }
}

// WithRetry задає, скільки разів повторювати безпечний запит після
// мережевого збою або відповіді 429/5xx; 0 вимикає повтори. Типово — 2.
func WithRetry(maxRetries int) Option {
	return func(c *Client) { c.maxRetries = max(maxRetries, 0) }
}

// WithLogger вмикає журнал запитів на рівні Debug: метод, шлях, спроба,
// статус і тривалість. Токен і тіла запитів у журнал не потрапляють.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) { c.logger = l }
}

// CallOption налаштовує окремий виклик API.
type CallOption func(*callOptions)

type callOptions struct {
	idempotencyKey string
	async          bool
}

// WithIdempotencyKey задає ключ ідемпотентності фіскальної операції. Ключ
// обирає інтеграція — найкраще похідний від замовлення ("order-10412"),
// щоб повтор після збою чи перезапуску не зареєстрував другий документ.
// Той самий ключ з іншою операцією — [ErrIdempotencyKeyReused].
// Для нефіскальних маршрутів не діє.
func WithIdempotencyKey(key string) CallOption {
	return func(o *callOptions) { o.idempotencyKey = key }
}

// WithAsync перемикає фіскальну операцію в асинхронний режим: API одразу
// повертає завдання в стані pending або processing, а результат приходить
// через опитування завдання або вебхук. Для нефіскальних маршрутів не діє.
func WithAsync() CallOption {
	return func(o *callOptions) { o.async = true }
}
