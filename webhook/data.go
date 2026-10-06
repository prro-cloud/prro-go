package webhook

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/prro-cloud/prro-go"
)

// TaskData — дані подій task.completed і task.failed.
type TaskData struct {
	TaskID string          `json:"task_id"`
	Type   prro.TaskType   `json:"type"`
	Status prro.TaskStatus `json:"status"`
	// CashRegisterID — каса завдання; порожній для завдань рівня клієнта.
	CashRegisterID string `json:"cash_register_id,omitempty"`
	// Result — той самий результат, що повертає [prro.TasksService.Get];
	// лише для task.completed.
	Result *prro.TaskResult `json:"result,omitempty"`
	// Error і Fault — опис збою; лише для task.failed.
	Error string      `json:"error,omitempty"`
	Fault *prro.Fault `json:"fault,omitempty"`
}

// ShiftData — дані подій shift.opened і shift.closed.
type ShiftData struct {
	CashRegisterID string `json:"cash_register_id"`
	ShiftID        string `json:"shift_id"`
	TaskID         string `json:"task_id"`
	// Testing — зміна тестова: каса в режимі test.
	Testing bool `json:"testing"`
}

// KeyData — дані події key.expiring.
type KeyData struct {
	KeyID string `json:"key_id"`
	// CashRegisterIDs — каси, що підписують цим ключем.
	CashRegisterIDs []string `json:"cash_register_ids,omitempty"`
	// CertSubject — власник сертифіката, як його вказав АЦСК.
	CertSubject string `json:"cert_subject,omitempty"`
	// CertValidTo — коли сертифікат спливає.
	CertValidTo time.Time `json:"cert_valid_to"`
}

// OfflineData — дані подій register.offline, register.online і
// register.offline_limit.
type OfflineData struct {
	CashRegisterID string    `json:"cash_register_id"`
	SessionID      string    `json:"session_id"`
	DPSSessionID   int64     `json:"dps_session_id"`
	StartedAt      time.Time `json:"started_at"`
	// EndedAt — кінець сесії; лише для register.online.
	EndedAt time.Time `json:"ended_at,omitzero"`
	// Documents — скільки документів видано в сесії; лише для
	// register.online.
	Documents int64 `json:"documents,omitempty"`
	// SessionUsed і MonthUsed — витрачений офлайн-час сесії і місяця; лише
	// для register.offline_limit (межі — 36 і 168 годин).
	SessionUsed time.Duration `json:"-"`
	MonthUsed   time.Duration `json:"-"`
}

// UnmarshalJSON розбирає тривалості, які сервіс передає рядками Go
// ("35h12m0s").
func (d *OfflineData) UnmarshalJSON(b []byte) error {
	type plain OfflineData
	var aux struct {
		*plain
		SessionUsed string `json:"session_used"`
		MonthUsed   string `json:"month_used"`
	}
	aux.plain = (*plain)(d)
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	for _, f := range []struct {
		dst *time.Duration
		src string
	}{{&d.SessionUsed, aux.SessionUsed}, {&d.MonthUsed, aux.MonthUsed}} {
		if f.src == "" {
			continue
		}
		v, err := time.ParseDuration(f.src)
		if err != nil {
			return fmt.Errorf("webhook: offline duration: %w", err)
		}
		*f.dst = v
	}
	return nil
}

// OfflineAbandonedData — дані події register.offline_abandoned: сесія, від
// якої відмовлено, і документи, яких ДПС так і не отримала. Чи видавати ці
// продажі знову, вирішує інтеграція.
type OfflineAbandonedData struct {
	CashRegisterID string                   `json:"cash_register_id"`
	SessionID      string                   `json:"session_id"`
	DPSSessionID   int64                    `json:"dps_session_id"`
	TaskID         string                   `json:"task_id"`
	Documents      []prro.AbandonedDocument `json:"documents"`
}

// RemediationData — дані події register.remediated: одне автоматичне
// виправлення розбіжності з ДПС.
type RemediationData struct {
	CashRegisterID string `json:"cash_register_id"`
	TaskID         string `json:"task_id,omitempty"`
	// Action — що виправлено: local_number_synced, shift_adopted,
	// shift_abandoned, zreport_recovered, zreport_autofiled,
	// register_config_filled.
	Action string `json:"action"`
	// FaultCode — збій, який спричинив виправлення.
	FaultCode string         `json:"fault_code"`
	Details   map[string]any `json:"details,omitempty"`
}

// AttentionData — дані події register.needs_attention: автоматичні
// виправлення не тримаються, на касу має поглянути людина.
type AttentionData struct {
	CashRegisterID string               `json:"cash_register_id"`
	Reason         prro.AttentionReason `json:"reason"`
	// Remedy і Count — яке виправлення повторюється і скільки разів; для
	// [prro.AttentionRecurringRemediation].
	Remedy string `json:"remedy,omitempty"`
	Count  int    `json:"count,omitempty"`
	// SessionID і Error — зупинена сесія і причина; для
	// [prro.AttentionOfflineSubmissionPaused].
	SessionID string `json:"session_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// BalanceData — дані події client.balance_low. Подія стосується клієнта,
// а не каси.
type BalanceData struct {
	Balance         prro.Kopecks `json:"balance"`
	SoftLimit       prro.Kopecks `json:"soft_limit"`
	PricePerReceipt prro.Kopecks `json:"price_per_receipt"`
	// ReceiptsLeft — скільки чеків ще вміщує залишок (Balance − SoftLimit).
	ReceiptsLeft int64 `json:"receipts_left"`
	// ThresholdReceipts — поріг, за яким надіслано попередження.
	ThresholdReceipts int `json:"threshold_receipts"`
}

// DPSData — дані подій system.dps_*: режим роботи з ДПС після переходу і
// його причина.
type DPSData struct {
	Verdict    prro.DPSMode `json:"verdict"`
	Override   string       `json:"override"`
	Effective  prro.DPSMode `json:"effective"`
	Cause      string       `json:"cause"`
	OccurredAt time.Time    `json:"occurred_at"`
}

// PingData — дані тестової відправки webhook.ping.
type PingData struct {
	Message   string    `json:"message"`
	WebhookID string    `json:"webhook_id"`
	SentAt    time.Time `json:"sent_at"`
}

// Task повертає дані події task.completed або task.failed.
func (e *Event) Task() (*TaskData, error) {
	return decode[TaskData](e, prro.EventTaskCompleted, prro.EventTaskFailed)
}

// Shift повертає дані події shift.opened або shift.closed.
func (e *Event) Shift() (*ShiftData, error) {
	return decode[ShiftData](e, prro.EventShiftOpened, prro.EventShiftClosed)
}

// Key повертає дані події key.expiring.
func (e *Event) Key() (*KeyData, error) {
	return decode[KeyData](e, prro.EventKeyExpiring)
}

// Offline повертає дані події register.offline, register.online або
// register.offline_limit.
func (e *Event) Offline() (*OfflineData, error) {
	return decode[OfflineData](e, prro.EventRegisterOffline, prro.EventRegisterOnline, prro.EventRegisterOfflineLimit)
}

// OfflineAbandoned повертає дані події register.offline_abandoned.
func (e *Event) OfflineAbandoned() (*OfflineAbandonedData, error) {
	return decode[OfflineAbandonedData](e, prro.EventRegisterOfflineAbandoned)
}

// Remediation повертає дані події register.remediated.
func (e *Event) Remediation() (*RemediationData, error) {
	return decode[RemediationData](e, prro.EventRegisterRemediated)
}

// Attention повертає дані події register.needs_attention.
func (e *Event) Attention() (*AttentionData, error) {
	return decode[AttentionData](e, prro.EventRegisterNeedsAttention)
}

// Balance повертає дані події client.balance_low.
func (e *Event) Balance() (*BalanceData, error) {
	return decode[BalanceData](e, prro.EventClientBalanceLow)
}

// DPS повертає дані подій system.dps_*.
func (e *Event) DPS() (*DPSData, error) {
	return decode[DPSData](e, prro.EventDPSOffline, prro.EventDPSOnline, prro.EventDPSFlapping, prro.EventDPSModeChanged)
}

// Ping повертає дані тестової відправки webhook.ping.
func (e *Event) Ping() (*PingData, error) {
	return decode[PingData](e, prro.EventPing)
}

// decode розбирає Data як T, якщо подія належить до types.
func decode[T any](e *Event, types ...prro.WebhookEvent) (*T, error) {
	if !slices.Contains(types, e.Type) {
		return nil, fmt.Errorf("webhook: event %q does not carry %T", e.Type, *new(T))
	}
	out := new(T)
	if err := json.Unmarshal(e.Data, out); err != nil {
		return nil, fmt.Errorf("webhook: decode %s data: %w", e.Type, err)
	}
	return out, nil
}
