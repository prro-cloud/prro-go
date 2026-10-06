package prro

import (
	"context"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// BillingService — передплачений баланс і тарифікація.
type BillingService service

// UsageParams — період підсумку тарифікації. Порожні межі — поточний
// календарний місяць за UTC.
type UsageParams struct {
	// From — початок періоду, включно.
	From Date
	// To — кінець періоду, не включно. Період понад 366 днів — 422.
	To Date
}

// BillingUsage — баланс і підсумок тарифікації за період. Суми — у
// копійках ([Kopecks]).
type BillingUsage struct {
	Balance Kopecks `json:"balance"`
	// SoftLimit — нижня межа балансу: поки Balance < SoftLimit, нові чеки
	// відхиляються з [ErrPaymentRequired].
	SoftLimit Kopecks `json:"soft_limit"`
	// Blocked — чеки зараз заблоковано: балансом або рішенням оператора.
	Blocked bool          `json:"blocked"`
	Period  BillingPeriod `json:"period"`
	// Receipts — кількість тарифікованих чеків за період.
	Receipts int64   `json:"receipts"`
	Charged  Kopecks `json:"charged"`
	Tariff   Tariff  `json:"tariff"`
}

// BillingPeriod — період підсумку: From включно, To не включно.
type BillingPeriod struct {
	From Date `json:"from"`
	To   Date `json:"to"`
}

// Tariff — тариф, за яким рахували.
type Tariff struct {
	PlanID          string  `json:"plan_id"`
	PricePerReceipt Kopecks `json:"price_per_receipt"`
}

// Usage повертає передплачений баланс і підсумок тарифікації за період;
// params може бути nil. Тарифікація асинхронна, тож підсумок може
// відставати від останнього чека на кілька секунд.
func (s *BillingService) Usage(ctx context.Context, params *UsageParams) (*BillingUsage, error) {
	q := url.Values{}
	if params != nil {
		if params.From != "" {
			q.Set("from", string(params.From))
		}
		if params.To != "" {
			q.Set("to", string(params.To))
		}
	}
	return call[BillingUsage](ctx, s.client, request{method: http.MethodGet, path: "/v1/billing/usage", query: q})
}

// InvoiceStatus — стан рахунка.
type InvoiceStatus string

// Стани рахунка.
const (
	InvoiceDraft   InvoiceStatus = "draft"   // чернетка
	InvoiceIssued  InvoiceStatus = "issued"  // виставлено
	InvoicePaid    InvoiceStatus = "paid"    // оплачено
	InvoiceOverdue InvoiceStatus = "overdue" // прострочено
)

// Invoice — рахунок за один розрахунковий період.
type Invoice struct {
	ID string `json:"id"`
	// PeriodStart і PeriodEnd — межі періоду, обидві включно.
	PeriodStart Date    `json:"period_start"`
	PeriodEnd   Date    `json:"period_end"`
	Amount      Kopecks `json:"amount"`
	// Currency — валюта за ISO 4217: "UAH".
	Currency  string        `json:"currency"`
	Status    InvoiceStatus `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
}

// InvoicesService — рахунки клієнта.
type InvoicesService service

// ListInvoicesParams — сторінка переліку рахунків.
type ListInvoicesParams struct {
	// Limit — скільки рахунків повернути: 1–200; 0 — типово 50.
	Limit int
	// Offset — скільки рахунків пропустити.
	Offset int
}

const maxPageSize = 200

// List повертає сторінку рахунків, від найновішого періоду; params може
// бути nil. Усі рахунки поспіль дає [InvoicesService.All].
func (s *InvoicesService) List(ctx context.Context, params *ListInvoicesParams) ([]Invoice, error) {
	q := url.Values{}
	if params != nil {
		if params.Limit > 0 {
			q.Set("limit", strconv.Itoa(params.Limit))
		}
		if params.Offset > 0 {
			q.Set("offset", strconv.Itoa(params.Offset))
		}
	}
	out, err := call[struct {
		Invoices []Invoice `json:"invoices"`
	}](ctx, s.client, request{method: http.MethodGet, path: "/v1/invoices", query: q})
	if err != nil {
		return nil, err
	}
	return out.Invoices, nil
}

// All обходить усі рахунки, від найновішого, підвантажуючи сторінки за
// потреби. Помилка завершує обхід:
//
//	for inv, err := range c.Invoices.All(ctx) {
//		if err != nil {
//			return err
//		}
//		fmt.Println(inv.PeriodStart, inv.Amount, inv.Status)
//	}
func (s *InvoicesService) All(ctx context.Context) iter.Seq2[Invoice, error] {
	return func(yield func(Invoice, error) bool) {
		for offset := 0; ; {
			page, err := s.List(ctx, &ListInvoicesParams{Limit: maxPageSize, Offset: offset})
			if err != nil {
				yield(Invoice{}, err)
				return
			}
			for _, inv := range page {
				if !yield(inv, nil) {
					return
				}
			}
			if len(page) < maxPageSize {
				return
			}
			offset += len(page)
		}
	}
}
