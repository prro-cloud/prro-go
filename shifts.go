package prro

import (
	"context"
	"net/http"
)

// ShiftsService — відкриття і закриття змін.
type ShiftsService service

// OpenShiftParams — необов'язкові параметри відкриття зміни.
type OpenShiftParams struct {
	// Cashier — ім'я касира, яке друкують у документі відкриття.
	Cashier string `json:"cashier,omitempty"`
}

// Open відкриває зміну на касі registerID. Якщо каса не в режимі
// [ShiftManual], крок можна пропустити — перший чек відкриє зміну сам.
// params може бути nil. Завдання, режими і ключ ідемпотентності — як у
// [ReceiptsService.Create].
func (s *ShiftsService) Open(ctx context.Context, registerID string, params *OpenShiftParams, opts ...CallOption) (*Task, error) {
	path, err := pathf("/v1/cash-registers/%s/shifts", registerID)
	if err != nil {
		return nil, err
	}
	r := request{method: http.MethodPost, path: path, fiscal: true}
	if params != nil {
		r.body = params
	}
	return call[Task](ctx, s.client, r, opts...)
}

// Close закриває поточну зміну каси registerID: однією операцією формує
// Z-звіт і документ закриття — обидва в Result.ZReport і Result.ShiftClose.
func (s *ShiftsService) Close(ctx context.Context, registerID string, opts ...CallOption) (*Task, error) {
	path, err := pathf("/v1/cash-registers/%s/shifts/current", registerID)
	if err != nil {
		return nil, err
	}
	return call[Task](ctx, s.client, request{method: http.MethodDelete, path: path, fiscal: true}, opts...)
}

// OfflineSessionsService — керування офлайн-сесією каси.
type OfflineSessionsService service

// CloseOfflineSessionParams — необов'язкові параметри завершення
// офлайн-сесії.
type CloseOfflineSessionParams struct {
	// Cashier — ім'я касира, яке друкують у документі завершення сесії.
	Cashier string `json:"cashier,omitempty"`
}

// Close передає пакети офлайн-сесії каси registerID до ДПС і завершує
// сесію, не чекаючи фонової передачі. Якщо ДПС недоступна, завдання
// завершується збоєм state.offline_close_needs_dps — сесію буде передано й
// завершено автоматично, щойно зв'язок відновиться. params може бути nil.
func (s *OfflineSessionsService) Close(ctx context.Context, registerID string, params *CloseOfflineSessionParams, opts ...CallOption) (*Task, error) {
	path, err := pathf("/v1/cash-registers/%s/offline-session/close", registerID)
	if err != nil {
		return nil, err
	}
	r := request{method: http.MethodPost, path: path, fiscal: true}
	if params != nil {
		r.body = params
	}
	return call[Task](ctx, s.client, r, opts...)
}

// Retry відновлює надсилання офлайн-пакетів, зупинене після того, як ДПС
// відхилила пакет сесії. Спершу усуньте причину. Повертає, скільки сесій
// повернуто до надсилання; 0 — зупинених не було.
func (s *OfflineSessionsService) Retry(ctx context.Context, registerID string) (int, error) {
	path, err := pathf("/v1/cash-registers/%s/offline-session/retry", registerID)
	if err != nil {
		return 0, err
	}
	out, err := call[struct {
		ResumedSessions int `json:"resumed_sessions"`
	}](ctx, s.client, request{method: http.MethodPost, path: path})
	if err != nil {
		return 0, err
	}
	return out.ResumedSessions, nil
}

// Abandon відмовляється від офлайн-сесії, яку ДПС не приймає. Доступно
// лише для сесії із зупиненим надсиланням і лише коли ДПС відповідає.
// Документи, яких ДПС не отримала, стають abandoned: їхніх фіскальних
// номерів у ДПС не існує. Перелік — у Result.OfflineAbandon; чи видавати
// ці продажі знову, вирішує інтеграція.
func (s *OfflineSessionsService) Abandon(ctx context.Context, registerID string, opts ...CallOption) (*Task, error) {
	path, err := pathf("/v1/cash-registers/%s/offline-session/abandon", registerID)
	if err != nil {
		return nil, err
	}
	return call[Task](ctx, s.client, request{method: http.MethodPost, path: path, fiscal: true}, opts...)
}
