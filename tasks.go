package prro

import (
	"context"
	"net/http"
	"time"
)

// TaskType — операція завдання.
type TaskType string

// Типи завдань.
const (
	TaskReceipt               TaskType = "receipt"                 // розрахунковий документ
	TaskOpenShift             TaskType = "open_shift"              // відкриття зміни
	TaskCloseShift            TaskType = "close_shift"             // Z-звіт і закриття зміни
	TaskCloseOfflineSession   TaskType = "close_offline_session"   // завершення офлайн-сесії
	TaskAbandonOfflineSession TaskType = "abandon_offline_session" // відмова від офлайн-сесії
	TaskSyncRegister          TaskType = "sync_register"           // звіряння з ДПС
)

// TaskStatus — стан завдання.
type TaskStatus string

// Стани завдання.
const (
	TaskPending    TaskStatus = "pending"    // у черзі
	TaskProcessing TaskStatus = "processing" // виконується
	TaskSucceeded  TaskStatus = "succeeded"  // завершено успішно, результат у Result
	TaskFailed     TaskStatus = "failed"     // завершено помилкою, опис у Fault
)

// Task — завдання фіскального конвеєра: у нього перетворюється кожна
// фіскальна операція (чек, зміна, офлайн-сесія).
type Task struct {
	// ID — ідентифікатор завдання; за ним опитують [TasksService.Get].
	ID         string     `json:"task_id"`
	Type       TaskType   `json:"type"`
	Status     TaskStatus `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt time.Time  `json:"finished_at,omitzero"`
	// Error — текст помилки завдання; структурований опис — у Fault.
	Error  string      `json:"error,omitempty"`
	Fault  *Fault      `json:"fault,omitempty"`
	Result *TaskResult `json:"result,omitempty"`
}

// Done повідомляє, чи завершилося завдання — успішно або помилкою.
func (t *Task) Done() bool { return t.Status == TaskSucceeded || t.Status == TaskFailed }

// TaskResult — результат завдання. Заповнені лише поля, що стосуються його
// типу: Receipt — для чека, ShiftOpen — для відкриття зміни, ZReport і
// ShiftClose — для закриття тощо.
type TaskResult struct {
	// CashRegisterID — каса, на якій виконано завдання.
	CashRegisterID string `json:"prro_id,omitempty"`
	// ShiftID — зміна, у межах якої видано документи завдання.
	ShiftID string `json:"shift_id,omitempty"`
	// ShiftNumber — наскрізний номер зміни в межах каси.
	ShiftNumber int64 `json:"shift_number,omitempty"`
	// Testing — документи тестові: каса в режимі test.
	Testing bool `json:"testing,omitempty"`

	Receipt        *DocResult            `json:"receipt,omitempty"`
	ShiftOpen      *DocResult            `json:"shift_open,omitempty"`
	ZReport        *DocResult            `json:"zreport,omitempty"`
	ShiftClose     *DocResult            `json:"shift_close,omitempty"`
	OfflineEnd     *DocResult            `json:"offline_end,omitempty"`
	OfflineAbandon *OfflineAbandonResult `json:"offline_abandon,omitempty"`
	Reconcile      *ReconcileReport      `json:"reconcile,omitempty"`
}

// DocResult — документ, який видало завдання: номери в журналі та в ДПС і
// посилання для покупця.
type DocResult struct {
	// DocumentID — ідентифікатор документа в сервісі.
	DocumentID string `json:"document_id"`
	// LocalNumber — наскрізний номер документа в межах каси (ORDERNUM).
	LocalNumber int64 `json:"local_number"`
	// FiscalNumber — фіскальний номер документа, присвоєний ДПС; в
	// офлайн-документа обчислений локально (<sid>.<n>.<crc>).
	FiscalNumber string `json:"fiscal_number"`
	// RegisterFiscalNumber — фіскальний номер каси («ФН ПРРО» на чеку);
	// присутній завжди, за ним інтеграція зіставляє записи з касою.
	RegisterFiscalNumber string `json:"register_fiscal_number"`
	// Offline — документ підписано в офлайн-сесії; у ДПС він з'явиться
	// після надсилання пакета сесії.
	Offline bool `json:"offline,omitempty"`
	// ReceiptURL — копія чека для покупця (https://r.prro.cloud/<код>),
	// відкривається без авторизації. Параметр ?format= обирає подання:
	// html (типово), png (576 px для термопринтера 80 мм) або qr. Є лише в
	// розрахункових документів.
	ReceiptURL string `json:"receipt_url,omitempty"`
	// TaxURL — той самий документ у кабінеті платника податків, незалежне
	// підтвердження фіскалізації. Відсутнє, доки документ не потрапив до ДПС.
	TaxURL string `json:"tax_url,omitempty"`
	// Original — початковий чек; лише в документа повернення.
	Original *ReturnOriginal `json:"original,omitempty"`
}

// ReturnOriginal — початковий чек повернення.
type ReturnOriginal struct {
	// FiscalNumber — фіскальний номер початкового чека.
	FiscalNumber string `json:"fiscal_number"`
	// DocumentID — початковий чек у журналі сервісу; порожній, якщо чек
	// видано поза сервісом.
	DocumentID string `json:"document_id,omitempty"`
	// Verified — повернення звірено із залишком за початковим чеком; false —
	// чек видано іншим ПРРО, повернення прийнято без звірки.
	Verified bool `json:"verified"`
}

// OfflineAbandonResult — результат відмови від офлайн-сесії.
type OfflineAbandonResult struct {
	// SessionID — сесія, від якої відмовлено.
	SessionID string `json:"session_id"`
	// DPSSessionID — номер сесії, виданий ДПС.
	DPSSessionID int64 `json:"dps_session_id"`
	// HeldDocuments — скільки документів сесії ДПС отримала; вони
	// лишаються зареєстрованими.
	HeldDocuments int64 `json:"held_documents"`
	// Abandoned — документи сесії, яких ДПС не отримала; чи видавати їх
	// знову, вирішує інтеграція.
	Abandoned []AbandonedDocument `json:"abandoned"`
	// ClosureSessionID — сесія, якою відмовлену сесію завершують у ДПС.
	ClosureSessionID string `json:"closure_session_id,omitempty"`
	// Closed — сесію завершено в ДПС, каса знову онлайн; false — завершення
	// ще передається.
	Closed bool `json:"closed"`
}

// AbandonedDocument — документ відмовленої офлайн-сесії. Його фіскального
// номера в ДПС не існує.
type AbandonedDocument struct {
	DocumentID string `json:"document_id"`
	TaskID     string `json:"task_id,omitempty"`
	// Class — вид документа: check, zreport, open_shift, close_shift,
	// offline_begin, offline_end.
	Class string `json:"class"`
	// Kind — що оформив чек; лише для Class = "check".
	Kind ReceiptType `json:"kind,omitempty"`
	// LocalNumber — локальний номер, який мав документ.
	LocalNumber int64 `json:"local_number"`
	// FiscalNumber — офлайн-фіскальний номер, надрукований на документі.
	FiscalNumber string `json:"fiscal_number,omitempty"`
	// Total — сума документа; лише для чеків.
	Total Amount `json:"total,omitempty"`
	// PayForm — форма оплати чека: cash, cashless або mixed.
	PayForm   string    `json:"pay_form,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ReconcileReport — підсумок звіряння реєстратора з ДПС: який стан
// побачили на боці ДПС і що виправили в сервісі.
type ReconcileReport struct {
	FiscalNumber       string `json:"fiscal_number"`
	DPSShiftOpened     bool   `json:"dps_shift_opened"`
	DPSNextLocalNumber int64  `json:"dps_next_local_number"`
	DPSTesting         bool   `json:"dps_testing,omitempty"`
	CounterOld         int64  `json:"counter_old,omitempty"`
	CounterNew         int64  `json:"counter_new,omitempty"`
	// ShiftAdopted — сервіс прийняв як свою зміну, відкриту на боці ДПС.
	ShiftAdopted bool `json:"shift_adopted,omitempty"`
	// ShiftAbandoned — сервіс закрив у себе зміну, якої на боці ДПС немає.
	ShiftAbandoned              bool `json:"shift_abandoned,omitempty"`
	OfflineCredentialsRefreshed bool `json:"offline_credentials_refreshed,omitempty"`
	// Cash — рух готівки зміни за обліком сервісу і за ДПС; довідково.
	Cash *ReconcileCash `json:"cash,omitempty"`
	// Totals — підсумки зміни за обліком сервісу і за ДПС.
	Totals *ReconcileTotals `json:"totals,omitempty"`
	// AttentionCleared — знято позначку auto_close_failing.
	AttentionCleared bool `json:"attention_cleared,omitempty"`
	// Skipped — чому звіряння нічого не змінювало ("offline_session").
	Skipped string `json:"skipped,omitempty"`
}

// ReconcileCash — готівка зміни за двома підрахунками.
type ReconcileCash struct {
	Ours  Amount `json:"ours"`
	DPS   Amount `json:"dps"`
	Match bool   `json:"match"`
}

// ReconcileTotals — підсумки зміни за двома обліками.
type ReconcileTotals struct {
	Ours ShiftTotalsBrief `json:"ours"`
	// DPS — відсутнє, якщо ДПС не повернула підсумки.
	DPS   *ShiftTotalsBrief `json:"dps,omitempty"`
	Match bool              `json:"match"`
	// ZReportSource — з яких підсумків буде подано Z-звіт: local або dps.
	ZReportSource string `json:"z_report_source"`
	// Forced — це звіряння перевело Z-звіт на підсумки ДПС.
	Forced bool `json:"forced,omitempty"`
	// Held — чому розбіжність не виправлено: dps_totals_unavailable,
	// shift_closing або offline_backlog.
	Held string `json:"held,omitempty"`
}

// ShiftTotalsBrief — суми й кількості чеків зміни, за якими ДПС звіряє
// Z-звіт.
type ShiftTotalsBrief struct {
	Sales         Amount `json:"sales"`
	SalesCount    int    `json:"sales_count"`
	Returns       Amount `json:"returns"`
	ReturnsCount  int    `json:"returns_count"`
	ServiceInput  Amount `json:"service_input"`
	ServiceOutput Amount `json:"service_output"`
}

// TasksService — стан і результат завдань.
type TasksService service

// Get повертає завдання в поточному стані. Завдання, що завершилося
// помилкою, — це успішна відповідь зі Status == [TaskFailed], а не помилка.
func (s *TasksService) Get(ctx context.Context, id string) (*Task, error) {
	path, err := pathf("/v1/tasks/%s", id)
	if err != nil {
		return nil, err
	}
	return call[Task](ctx, s.client, request{method: http.MethodGet, path: path})
}

// Wait опитує завдання, доки воно не завершиться, з паузою, що зростає від
// 0,5 до 5 секунд. Межу очікування задає ctx.
//
// Завдання, що завершилося помилкою, повертається разом із [*Error], як у
// синхронній відповіді: errors.Is(err, [ErrTaskFailed]), опис збою — у
// [Error.Fault].
func (s *TasksService) Wait(ctx context.Context, id string) (*Task, error) {
	interval := s.client.pollMin
	for {
		task, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		switch task.Status {
		case TaskSucceeded:
			return task, nil
		case TaskFailed:
			return task, &Error{
				StatusCode: http.StatusUnprocessableEntity,
				Code:       codeTaskFailed,
				Message:    task.Error,
				TaskID:     task.ID,
				Fault:      task.Fault,
			}
		}
		if err := sleep(ctx, interval); err != nil {
			return nil, err
		}
		interval = min(interval*3/2, s.client.pollMax)
	}
}
