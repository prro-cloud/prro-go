package prro

import (
	"log/slog"
	"net/http"
	"runtime"
	"time"
)

// Version — версія бібліотеки; потрапляє в заголовок User-Agent.
const Version = "0.1.0"

// APIVersion — версія контракту API (info.version в openapi.yaml), з якою
// зібрано бібліотеку.
const APIVersion = "0.5.0"

// DefaultBaseURL — базова адреса машинного API PRRO.cloud.
const DefaultBaseURL = "https://api.prro.cloud"

const (
	// Синхронний режим тримає з'єднання до відповіді ДПС до 30 секунд,
	// тож тайм-аут HTTP-клієнта за замовчуванням має запас понад це.
	defaultTimeout    = 60 * time.Second
	defaultMaxRetries = 2
	defaultRetryMin   = 500 * time.Millisecond
	defaultRetryMax   = 8 * time.Second
	// Межі паузи між опитуваннями в TasksService.Wait.
	defaultPollMin = 500 * time.Millisecond
	defaultPollMax = 5 * time.Second
)

var defaultUserAgent = "prro-go/" + Version + " (" + runtime.Version() + "; " + runtime.GOOS + "/" + runtime.GOARCH + ")"

// Client — клієнт машинного API PRRO.cloud. Створюється через [NewClient];
// безпечний для одночасного використання з кількох горутин.
type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
	userAgent  string
	language   string
	maxRetries int
	retryMin   time.Duration
	retryMax   time.Duration
	pollMin    time.Duration
	pollMax    time.Duration
	logger     *slog.Logger

	common service

	// Receipts — реєстрація чеків.
	Receipts *ReceiptsService
	// Tasks — стан і результат фіскальних завдань.
	Tasks *TasksService
	// CashRegisters — каси, доступні токену, і їхній стан.
	CashRegisters *CashRegistersService
	// Shifts — відкриття і закриття змін.
	Shifts *ShiftsService
	// OfflineSessions — керування офлайн-сесією каси.
	OfflineSessions *OfflineSessionsService
	// Billing — баланс і тарифікація.
	Billing *BillingService
	// Invoices — рахунки клієнта.
	Invoices *InvoicesService
	// Webhooks — вебхуки та історія доставок.
	Webhooks *WebhooksService
	// System — службові маршрути: версія сервісу, дозволи токена, режим
	// роботи з ДПС.
	System *SystemService
}

// service — спільна основа груп маршрутів (System, Receipts, …).
type service struct{ client *Client }

// NewClient створює клієнт, що автентифікується машинним токеном token.
// Без опцій клієнт звертається до [DefaultBaseURL], чекає на відповідь до
// 60 секунд і повторює безпечні запити двічі.
func NewClient(token string, opts ...Option) *Client {
	c := &Client{
		token:      token,
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
		userAgent:  defaultUserAgent,
		maxRetries: defaultMaxRetries,
		retryMin:   defaultRetryMin,
		retryMax:   defaultRetryMax,
		pollMin:    defaultPollMin,
		pollMax:    defaultPollMax,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.common.client = c
	c.Receipts = (*ReceiptsService)(&c.common)
	c.Tasks = (*TasksService)(&c.common)
	c.CashRegisters = (*CashRegistersService)(&c.common)
	c.Shifts = (*ShiftsService)(&c.common)
	c.OfflineSessions = (*OfflineSessionsService)(&c.common)
	c.Billing = (*BillingService)(&c.common)
	c.Invoices = (*InvoicesService)(&c.common)
	c.Webhooks = (*WebhooksService)(&c.common)
	c.System = (*SystemService)(&c.common)
	return c
}
