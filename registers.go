package prro

import (
	"context"
	"net/http"
	"time"
)

// RegisterMode — режим каси.
type RegisterMode string

// Режими каси.
const (
	RegisterProduction RegisterMode = "production" // бойовий режим
	RegisterTest       RegisterMode = "test"       // документи позначені як тестові
)

// RegisterState — стан каси.
type RegisterState string

// Стани каси.
const (
	RegisterClosed  RegisterState = "closed"  // зміна закрита
	RegisterOpened  RegisterState = "opened"  // зміна відкрита
	RegisterOffline RegisterState = "offline" // триває офлайн-сесія
)

// ShiftMode — як керують змінами каси.
type ShiftMode string

// Режими змін.
const (
	// ShiftManual — клієнт відкриває і закриває зміни сам.
	ShiftManual ShiftMode = "manual"
	// ShiftRoundTheClock — зміна закривається в кожен із ShiftCloseTimes,
	// наступний чек відкриває нову.
	ShiftRoundTheClock ShiftMode = "round_the_clock"
	// ShiftWorkingHours — денне вікно між двома межами ShiftCloseTimes.
	ShiftWorkingHours ShiftMode = "working_hours"
)

// ShiftStatus — стан зміни.
type ShiftStatus string

// Стани зміни; opening і closing — операція ще в роботі.
const (
	ShiftOpening ShiftStatus = "opening"
	ShiftOpened  ShiftStatus = "opened"
	ShiftClosing ShiftStatus = "closing"
	ShiftClosed  ShiftStatus = "closed"
)

// AttentionReason — чому каса потребує уваги.
type AttentionReason string

// Причини позначки «потребує уваги».
const (
	// AttentionRecurringRemediation — розбіжності з ДПС усуваються знову і
	// знову.
	AttentionRecurringRemediation AttentionReason = "recurring_remediation"
	// AttentionOfflineSubmissionPaused — надсилання офлайн-пакетів
	// зупинено; відновлюють через [OfflineSessionsService.Retry].
	AttentionOfflineSubmissionPaused AttentionReason = "offline_submission_paused"
	// AttentionAutoCloseFailing — не вдається автоматичне закриття зміни.
	AttentionAutoCloseFailing AttentionReason = "auto_close_failing"
)

// CashRegister — каса в переліку: ідентифікатор для звернень і реквізити,
// за якими її впізнає людина.
type CashRegister struct {
	// ID — ідентифікатор каси в API: CashRegisterID чека і параметр решти
	// методів. Фіскальний номер для цього не годиться.
	ID string `json:"id"`
	// FiscalNumber — фіскальний номер ПРРО, присвоєний ДПС.
	FiscalNumber string `json:"fiscal_number"`
	// LocalNumber — власний номер каси у клієнта.
	LocalNumber  string        `json:"local_number"`
	OrgName      string        `json:"org_name,omitempty"`      // назва суб'єкта господарювання
	PointName    string        `json:"point_name,omitempty"`    // назва точки продажу
	PointAddress string        `json:"point_address,omitempty"` // адреса точки продажу
	Mode         RegisterMode  `json:"mode"`
	State        RegisterState `json:"state"`
}

// CashRegisterState — поточний стан каси: режим, зміна, готівка,
// офлайн-сесія.
type CashRegisterState struct {
	ID           string        `json:"id"`
	FiscalNumber string        `json:"fiscal_number"`
	LocalNumber  string        `json:"local_number"`
	Mode         RegisterMode  `json:"mode"`
	State        RegisterState `json:"state"`
	// OfflineReady — каса має видані ДПС офлайн-реквізити.
	OfflineReady bool      `json:"offline_ready,omitempty"`
	ShiftMode    ShiftMode `json:"shift_mode,omitempty"`
	// ShiftCloseTimes — час щоденного закриття зміни за київським часом
	// ("21:00"); порожній для [ShiftManual].
	ShiftCloseTimes []string     `json:"shift_close_times,omitempty"`
	CashBalance     *CashBalance `json:"cash_balance,omitempty"`
	// CurrentShift — поточна зміна; nil після закриття.
	CurrentShift *Shift `json:"current_shift,omitempty"`
	// ShiftTotals — підсумки відкритої зміни, з яких буде побудовано Z-звіт.
	ShiftTotals *ShiftTotals `json:"shift_totals,omitempty"`
	// OfflineSession — присутня, поки каса працює офлайн.
	OfflineSession *OfflineSession `json:"offline_session,omitempty"`
	// OfflineLimits — законодавчі межі офлайн-роботи.
	OfflineLimits OfflineLimits `json:"offline_limits"`
	// OfflineMonthUsedSeconds — час офлайн-роботи в цьому календарному
	// місяці (за київським часом), с.
	OfflineMonthUsedSeconds int64 `json:"offline_month_used_seconds,omitempty"`
	// Attention — присутня, коли каса потребує уваги; знімається через
	// [CashRegistersService.ClearAttention].
	Attention *Attention `json:"attention,omitempty"`
}

// Shift — зміна каси.
type Shift struct {
	ID string `json:"id"`
	// Number — наскрізний номер зміни в межах каси («Зміна №147»).
	Number int64       `json:"number"`
	Status ShiftStatus `json:"status"`
	// Testing — зміну відкрито в режимі test, усі її документи тестові.
	Testing  bool      `json:"testing,omitempty"`
	OpenedAt time.Time `json:"opened_at,omitzero"`
	// Documents — скільки документів створила зміна, усіх видів.
	Documents int `json:"documents,omitempty"`
}

// CashBalance — лічильник готівки каси. Враховує лише документи, що
// пройшли через сервіс; від'ємне значення означає, що в касі було більше,
// ніж побачив сервіс.
type CashBalance struct {
	Amount Amount `json:"amount"`
	// AsOf — коли залишок рухався востаннє; нульовий час — рухів ще не
	// було, тож нуль означає «немає даних», а не «каса порожня».
	AsOf time.Time `json:"as_of,omitzero"`
}

// ShiftTotals — поточні підсумки відкритої зміни. Сторно віднімається з
// реалізації; службові внесення й видачі не належать до жодного боку.
type ShiftTotals struct {
	Sales      ShiftTotalsSide `json:"realiz"`
	Returns    ShiftTotalsSide `json:"return"`
	ServiceIn  Amount          `json:"service_in"`
	ServiceOut Amount          `json:"service_out"`
}

// ShiftTotalsSide — один бік підсумків зміни: кількість документів, сума і
// розклад за формами оплати.
type ShiftTotalsSide struct {
	Count    int            `json:"count"`
	Sum      Amount         `json:"sum"`
	PayForms []PayFormTotal `json:"pay_forms,omitempty"`
}

// PayFormTotal — накопичення за однією формою оплати, як у Z-звіті.
type PayFormTotal struct {
	Code int    `json:"code"` // PAYFORMCD
	Name string `json:"name"` // PAYFORMNM
	Sum  Amount `json:"sum"`
}

// OfflineSession — офлайн-сесія каси.
type OfflineSession struct {
	ID string `json:"id"`
	// DPSSessionID — номер сесії, виданий ДПС; входить у фіскальний номер
	// офлайн-документа.
	DPSSessionID int64 `json:"dps_session_id"`
	// StartedAt — початок сесії; від нього рахують 36-годинну межу.
	StartedAt time.Time `json:"started_at"`
	Documents int64     `json:"documents"`
	// LastSignificantAt — останній документ, що продовжує сесію.
	LastSignificantAt time.Time `json:"last_significant_at,omitzero"`
}

// OfflineLimits — законодавчі межі офлайн-роботи: 36 годин на сесію і 168
// годин на календарний місяць, у секундах.
type OfflineLimits struct {
	SessionSeconds int64 `json:"session_seconds"`
	MonthSeconds   int64 `json:"month_seconds"`
}

// Attention — позначка «каса потребує уваги».
type Attention struct {
	Reason AttentionReason `json:"reason"`
	Since  time.Time       `json:"since,omitzero"`
}

// CashBalanceDeclaration — лічильник готівки до заяви, після неї і
// поправка між ними.
type CashBalanceDeclaration struct {
	Amount     Amount `json:"amount"`
	Previous   Amount `json:"previous"`
	Adjustment Amount `json:"adjustment"`
}

// CashRegistersService — каси, доступні токену, і їхній стан.
type CashRegistersService service

// List повертає каси, доступні токену, від найстаріших. Звідси беруть
// ідентифікатор каси для решти методів — збережіть його в налаштуваннях
// інтеграції.
func (s *CashRegistersService) List(ctx context.Context) ([]CashRegister, error) {
	out, err := call[struct {
		CashRegisters []CashRegister `json:"cash_registers"`
	}](ctx, s.client, request{method: http.MethodGet, path: "/v1/cash-registers"})
	if err != nil {
		return nil, err
	}
	return out.CashRegisters, nil
}

// Get повертає поточний стан каси id.
func (s *CashRegistersService) Get(ctx context.Context, id string) (*CashRegisterState, error) {
	path, err := pathf("/v1/cash-registers/%s", id)
	if err != nil {
		return nil, err
	}
	return call[CashRegisterState](ctx, s.client, request{method: http.MethodGet, path: path})
}

// ClearAttention знімає з каси позначку «потребує уваги». false — позначки
// й не було. Зупинене надсилання офлайн-пакетів додатково відновлюють
// через [OfflineSessionsService.Retry].
func (s *CashRegistersService) ClearAttention(ctx context.Context, id string) (bool, error) {
	path, err := pathf("/v1/cash-registers/%s/attention", id)
	if err != nil {
		return false, err
	}
	out, err := call[struct {
		Cleared bool `json:"cleared"`
	}](ctx, s.client, request{method: http.MethodDelete, path: path})
	if err != nil {
		return false, err
	}
	return out.Cleared, nil
}

// SetCashBalance заявляє фактичний залишок готівки: переставляє лічильник
// на перераховану суму — коли касу підключили з непорожнім ящиком або
// лічильник розійшовся з фактом. Операція не фіскальна; рух готівки
// реєструють [ServiceReceipt]. Від'ємна сума — [ErrValidation].
func (s *CashRegistersService) SetCashBalance(ctx context.Context, id string, amount Amount) (*CashBalanceDeclaration, error) {
	path, err := pathf("/v1/cash-registers/%s/cash-balance", id)
	if err != nil {
		return nil, err
	}
	return call[CashBalanceDeclaration](ctx, s.client, request{
		method: http.MethodPut, path: path, body: map[string]Amount{"amount": amount},
	})
}
