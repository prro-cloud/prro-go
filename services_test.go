package prro

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// captured — запит, який отримав тестовий сервер.
type captured struct {
	Method, Path, Query, Key string
	Body                     string
}

// fields розбирає тіло запиту як JSON-об'єкт.
func (r captured) fields(t *testing.T) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal([]byte(r.Body), &m); err != nil {
		t.Fatalf("body %q: %v", r.Body, err)
	}
	return m
}

// stub — клієнт до сервера, що на кожен запит віддає status і body, а сам
// запит передає в канал.
func stub(t *testing.T, status int, body string) (*Client, <-chan captured) {
	t.Helper()
	reqs := make(chan captured, 16)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		reqs <- captured{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("Idempotency-Key"), string(b)}
		if status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		writeJSON(w, status, body)
	})
	return c, reqs
}

func expect(t *testing.T, got captured, method, path, query string) {
	t.Helper()
	if got.Method != method || got.Path != path || got.Query != query {
		t.Errorf("request = %s %s?%s, want %s %s?%s", got.Method, got.Path, got.Query, method, path, query)
	}
}

const receiptTask = `{
	"task_id": "0197a2c1-0000-7000-8000-000000000001",
	"type": "receipt",
	"status": "succeeded",
	"created_at": "2026-08-03T01:07:53Z",
	"finished_at": "2026-08-03T01:07:54Z",
	"result": {
		"prro_id": "reg-1",
		"shift_id": "shift-1",
		"shift_number": 147,
		"receipt": {
			"document_id": "0197a2c3-0000-7000-8000-000000000002",
			"local_number": 42,
			"fiscal_number": "7466800082",
			"register_fiscal_number": "4001063533",
			"receipt_url": "https://r.prro.cloud/aB3xK9pQvT2mNr7c",
			"tax_url": "https://cabinet.tax.gov.ua/cashregs/check?fn=4001063533&id=7466800082"
		}
	}
}`

func TestReceiptsCreateSettlement(t *testing.T) {
	c, reqs := stub(t, http.StatusCreated, receiptTask)
	task, err := c.Receipts.Create(context.Background(), &SettlementReceipt{
		CashRegisterID: "reg-1",
		Type:           ReceiptSale,
		Items:          []ReceiptItem{{Name: "Кава американо", Quantity: "1", Price: "65.00", TaxLetters: "А"}},
		Payments:       []Payment{{Type: PaymentCard, Sum: AmountFromKop(6500)}},
	}, WithIdempotencyKey("order-10412"))
	if err != nil {
		t.Fatal(err)
	}

	r := <-reqs
	expect(t, r, http.MethodPost, "/v1/receipts", "")
	if r.Key != "order-10412" {
		t.Errorf("Idempotency-Key = %q", r.Key)
	}
	want := `{"cash_register_id":"reg-1","type":"sale",` +
		`"items":[{"name":"Кава американо","quantity":"1","price":"65.00","tax_letters":"А"}],` +
		`"payments":[{"type":"card","sum":"65.00"}]}`
	if r.Body != want {
		t.Errorf("body =\n%s\nwant\n%s", r.Body, want)
	}

	if !task.Done() || task.Type != TaskReceipt || task.Result.ShiftNumber != 147 {
		t.Fatalf("task = %+v", task)
	}
	doc := task.Result.Receipt
	if doc.FiscalNumber != "7466800082" || doc.LocalNumber != 42 || !strings.HasPrefix(doc.ReceiptURL, "https://r.prro.cloud/") {
		t.Errorf("receipt = %+v", doc)
	}
}

func TestReceiptsCreateService(t *testing.T) {
	c, reqs := stub(t, http.StatusAccepted, `{"task_id":"t1","type":"receipt","status":"pending","created_at":"2026-08-03T01:07:53Z"}`)
	task, err := c.Receipts.Create(context.Background(), &ServiceReceipt{
		CashRegisterID: "reg-1", Type: ReceiptServiceOut, Sum: "1500.00", Comment: "Інкасація",
	}, WithAsync())
	if err != nil {
		t.Fatal(err)
	}
	r := <-reqs
	expect(t, r, http.MethodPost, "/v1/receipts", "mode=async")
	if !uuidV7.MatchString(r.Key) {
		t.Errorf("auto Idempotency-Key = %q", r.Key)
	}
	f := r.fields(t)
	if _, ok := f["items"]; ok || f["sum"] != "1500.00" || f["type"] != "service_out" {
		t.Errorf("body = %s", r.Body)
	}
	if task.Done() || task.Status != TaskPending {
		t.Errorf("task = %+v", task)
	}
}

func TestReceiptsCreateRejectsBadInput(t *testing.T) {
	c, reqs := stub(t, http.StatusCreated, receiptTask)
	inputs := map[string]ReceiptInput{
		"nil":                nil,
		"typed nil":          (*SettlementReceipt)(nil),
		"settlement as svc":  &SettlementReceipt{Type: ReceiptServiceIn},
		"service as sale":    &ServiceReceipt{Type: ReceiptSale},
		"settlement no type": &SettlementReceipt{},
	}
	for name, in := range inputs {
		if _, err := c.Receipts.Create(context.Background(), in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if len(reqs) != 0 {
		t.Errorf("%d requests sent for invalid input", len(reqs))
	}
}

func TestReceiptReturnBody(t *testing.T) {
	b, err := json.Marshal(&SettlementReceipt{
		CashRegisterID: "reg-1",
		Type:           ReceiptReturn,
		SoldAt:         time.Date(2026, 9, 3, 18, 11, 0, 0, time.UTC),
		Original:       &ReceiptOriginal{FiscalNumber: "7466800082", Date: "2026-09-01"},
		Items: []ReceiptItem{{
			Name: "Кава", Quantity: "0.750", Price: "65.00",
			Discount: &Discount{Sum: "5.00", Percent: "10"},
		}},
		Payments: []Payment{{
			Type: PaymentCard, Sum: "60.00", Means: "ПЕРЕКАЗ З КАРТКИ",
			Card: &CardDetails{AuthCode: "123456", TransactionDate: time.Date(2026, 9, 3, 18, 10, 0, 0, time.UTC)},
		}},
		RoundingStep: "0.10",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		`"sold_at":"2026-09-03T18:11:00Z"`,
		`"original":{"fiscal_number":"7466800082","date":"2026-09-01"}`,
		`"discount":{"sum":"5.00","percent":"10"}`,
		`"card":{"transaction_date":"2026-09-03T18:10:00Z","auth_code":"123456"}`,
		`"rounding_step":"0.10"`,
	} {
		if !strings.Contains(string(b), part) {
			t.Errorf("body %s\nmissing %s", b, part)
		}
	}
}

func TestTasksWait(t *testing.T) {
	statuses := []string{"pending", "processing", "succeeded"}
	var n atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tasks/t1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		i := min(int(n.Add(1))-1, len(statuses)-1)
		writeJSON(w, 200, fmt.Sprintf(`{"task_id":"t1","type":"open_shift","status":%q,"created_at":"2026-08-03T01:07:53Z"}`, statuses[i]))
	})
	task, err := c.Tasks.Wait(context.Background(), "t1")
	if err != nil || task.Status != TaskSucceeded {
		t.Fatalf("task = %+v, err = %v", task, err)
	}
}

func TestTasksWaitFailed(t *testing.T) {
	c, _ := stub(t, 200, `{"task_id":"t1","type":"receipt","status":"failed","created_at":"2026-08-03T01:07:53Z",
		"error":"DPS rejected","fault":{"code":"dps.check_local_number_invalid","class":"resync"}}`)
	task, err := c.Tasks.Wait(context.Background(), "t1")
	var apiErr *Error
	if !errors.Is(err, ErrTaskFailed) || !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if task == nil || task.Status != TaskFailed || apiErr.TaskID != "t1" || apiErr.Fault.Class != FaultResync {
		t.Errorf("task = %+v, err = %+v", task, apiErr)
	}
}

func TestTasksWaitDeadline(t *testing.T) {
	c, _ := stub(t, 200, `{"task_id":"t1","type":"receipt","status":"processing","created_at":"2026-08-03T01:07:53Z"}`)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Tasks.Wait(ctx, "t1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestEmptyIDAndEscaping(t *testing.T) {
	c, reqs := stub(t, 200, `{"id":"a/b","fiscal_number":"1","local_number":"1","mode":"test","state":"closed",
		"offline_limits":{"session_seconds":129600,"month_seconds":604800}}`)
	if _, err := c.Tasks.Get(context.Background(), ""); !errors.Is(err, errEmptyID) {
		t.Errorf("empty id: err = %v", err)
	}
	if _, err := c.Webhooks.Replay(context.Background(), "w1", ""); !errors.Is(err, errEmptyID) {
		t.Errorf("empty delivery id: err = %v", err)
	}
	if len(reqs) != 0 {
		t.Fatal("request sent for empty id")
	}
	if _, err := c.CashRegisters.Get(context.Background(), "a/b"); err != nil {
		t.Fatal(err)
	}
	expect(t, <-reqs, http.MethodGet, "/v1/cash-registers/a%2Fb", "")
}

func TestCashRegisters(t *testing.T) {
	ctx := context.Background()

	c, reqs := stub(t, 200, `{"cash_registers":[{"id":"reg-1","fiscal_number":"4001063533","local_number":"1",
		"org_name":"ФОП Іваненко","mode":"production","state":"opened"}]}`)
	list, err := c.CashRegisters.List(ctx)
	if err != nil || len(list) != 1 || list[0].State != RegisterOpened || list[0].OrgName != "ФОП Іваненко" {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
	expect(t, <-reqs, http.MethodGet, "/v1/cash-registers", "")

	c, reqs = stub(t, 200, `{
		"id":"reg-1","fiscal_number":"4001063533","local_number":"1","mode":"production","state":"opened",
		"shift_mode":"round_the_clock","shift_close_times":["09:00","21:00"],
		"cash_balance":{"amount":"1500.00","as_of":"2026-08-03T01:07:54Z"},
		"current_shift":{"id":"s1","number":147,"status":"opened","opened_at":"2026-08-03T01:00:00Z","documents":12},
		"shift_totals":{"realiz":{"count":3,"sum":"195.00","pay_forms":[{"code":0,"name":"ГОТІВКА","sum":"130.00"}]},
			"return":{"count":0,"sum":"0.00"},"service_in":"0.00","service_out":"0.00"},
		"offline_limits":{"session_seconds":129600,"month_seconds":604800},
		"attention":{"reason":"auto_close_failing","since":"2026-08-02T21:00:00Z"}}`)
	st, err := c.CashRegisters.Get(ctx, "reg-1")
	if err != nil {
		t.Fatal(err)
	}
	expect(t, <-reqs, http.MethodGet, "/v1/cash-registers/reg-1", "")
	if st.ShiftMode != ShiftRoundTheClock || st.CurrentShift.Number != 147 || st.CashBalance.Amount != "1500.00" ||
		st.ShiftTotals.Sales.PayForms[0].Name != "ГОТІВКА" || st.OfflineLimits.SessionSeconds != 36*3600 ||
		st.Attention.Reason != AttentionAutoCloseFailing || st.OfflineSession != nil {
		t.Errorf("state = %+v", st)
	}

	c, reqs = stub(t, 200, `{"cleared":true}`)
	if ok, err := c.CashRegisters.ClearAttention(ctx, "reg-1"); err != nil || !ok {
		t.Fatalf("cleared = %v, err = %v", ok, err)
	}
	expect(t, <-reqs, http.MethodDelete, "/v1/cash-registers/reg-1/attention", "")

	c, reqs = stub(t, 200, `{"amount":"1500.00","previous":"-20.00","adjustment":"1520.00"}`)
	decl, err := c.CashRegisters.SetCashBalance(ctx, "reg-1", "1500.00")
	if err != nil || decl.Adjustment != "1520.00" {
		t.Fatalf("decl = %+v, err = %v", decl, err)
	}
	r := <-reqs
	expect(t, r, http.MethodPut, "/v1/cash-registers/reg-1/cash-balance", "")
	if r.Body != `{"amount":"1500.00"}` || r.Key != "" {
		t.Errorf("body = %s, key = %q", r.Body, r.Key)
	}
}

func TestShiftsAndOfflineSessions(t *testing.T) {
	ctx := context.Background()
	task := `{"task_id":"t1","type":"open_shift","status":"succeeded","created_at":"2026-08-03T01:07:53Z"}`

	tests := []struct {
		name               string
		call               func(c *Client) error
		method, path, body string
		fiscal             bool
	}{
		{
			"open nil", func(c *Client) error { _, err := c.Shifts.Open(ctx, "r1", nil); return err },
			http.MethodPost, "/v1/cash-registers/r1/shifts", "", true,
		},
		{"open cashier", func(c *Client) error {
			_, err := c.Shifts.Open(ctx, "r1", &OpenShiftParams{Cashier: "Каса самообслуговування"})
			return err
		}, http.MethodPost, "/v1/cash-registers/r1/shifts", `{"cashier":"Каса самообслуговування"}`, true},
		{
			"close", func(c *Client) error { _, err := c.Shifts.Close(ctx, "r1"); return err },
			http.MethodDelete, "/v1/cash-registers/r1/shifts/current", "", true,
		},
		{"offline close", func(c *Client) error {
			_, err := c.OfflineSessions.Close(ctx, "r1", &CloseOfflineSessionParams{Cashier: "Олена"})
			return err
		}, http.MethodPost, "/v1/cash-registers/r1/offline-session/close", `{"cashier":"Олена"}`, true},
		{
			"offline abandon", func(c *Client) error { _, err := c.OfflineSessions.Abandon(ctx, "r1"); return err },
			http.MethodPost, "/v1/cash-registers/r1/offline-session/abandon", "", true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, reqs := stub(t, http.StatusCreated, task)
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
			r := <-reqs
			expect(t, r, tt.method, tt.path, "")
			if r.Body != tt.body || (r.Key != "") != tt.fiscal {
				t.Errorf("body = %q, key = %q", r.Body, r.Key)
			}
		})
	}

	c, reqs := stub(t, 200, `{"resumed_sessions":2}`)
	if n, err := c.OfflineSessions.Retry(ctx, "r1"); err != nil || n != 2 {
		t.Fatalf("resumed = %d, err = %v", n, err)
	}
	r := <-reqs
	expect(t, r, http.MethodPost, "/v1/cash-registers/r1/offline-session/retry", "")
	if r.Key != "" {
		t.Errorf("retry carries Idempotency-Key %q", r.Key)
	}
}

func TestOfflineAbandonResult(t *testing.T) {
	c, _ := stub(t, http.StatusCreated, `{"task_id":"t1","type":"abandon_offline_session","status":"succeeded",
		"created_at":"2026-08-03T01:07:53Z","result":{"offline_abandon":{"session_id":"s1","dps_session_id":77,
		"held_documents":3,"closed":true,"abandoned":[{"document_id":"d1","class":"check","kind":"sale",
		"local_number":45,"fiscal_number":"77.4.1234","total":"65.00","pay_form":"cash","created_at":"2026-08-02T10:00:00Z"}]}}}`)
	task, err := c.OfflineSessions.Abandon(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	ab := task.Result.OfflineAbandon
	if !ab.Closed || len(ab.Abandoned) != 1 || ab.Abandoned[0].Kind != ReceiptSale || ab.Abandoned[0].Total != "65.00" {
		t.Errorf("abandon = %+v", ab)
	}
}

func TestBillingUsage(t *testing.T) {
	c, reqs := stub(t, 200, `{"balance":"150000.0000","soft_limit":"0.0000","blocked":false,
		"period":{"from":"2026-07-01","to":"2026-08-01"},"receipts":1200,"charged":"60000.0000",
		"tariff":{"plan_id":"p1","price_per_receipt":"50.0000"}}`)
	u, err := c.Billing.Usage(context.Background(), &UsageParams{From: "2026-07-01", To: DateOf(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, <-reqs, http.MethodGet, "/v1/billing/usage", "from=2026-07-01&to=2026-08-01")
	if u.Balance != "150000.0000" || u.Receipts != 1200 || u.Tariff.PricePerReceipt != "50.0000" || u.Period.To != "2026-08-01" {
		t.Errorf("usage = %+v", u)
	}

	c, reqs = stub(t, 200, `{"balance":"0","soft_limit":"0","blocked":true,"period":{"from":"2026-10-01","to":"2026-11-01"},
		"receipts":0,"charged":"0","tariff":{"plan_id":"p1","price_per_receipt":"50"}}`)
	if _, err := c.Billing.Usage(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	expect(t, <-reqs, http.MethodGet, "/v1/billing/usage", "")
}

func invoicesPage(n, from int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf(`{"id":"inv-%d","period_start":"2026-01-01","period_end":"2026-01-31",
			"amount":"100.0000","currency":"UAH","status":"paid","created_at":"2026-02-01T00:00:00Z"}`, from+i)
	}
	return `{"invoices":[` + strings.Join(items, ",") + `]}`
}

func TestInvoices(t *testing.T) {
	c, reqs := stub(t, 200, invoicesPage(2, 0))
	list, err := c.Invoices.List(context.Background(), &ListInvoicesParams{Limit: 2, Offset: 4})
	if err != nil || len(list) != 2 || list[0].Status != InvoicePaid || list[0].PeriodEnd != "2026-01-31" {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
	expect(t, <-reqs, http.MethodGet, "/v1/invoices", "limit=2&offset=4")

	var mu sync.Mutex
	var queries []string
	pages := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return queries
	}
	c = newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset == 0 {
			writeJSON(w, 200, invoicesPage(maxPageSize, 0))
			return
		}
		writeJSON(w, 200, invoicesPage(5, offset))
	})
	var ids []string
	for inv, err := range c.Invoices.All(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, inv.ID)
	}
	if len(ids) != maxPageSize+5 || ids[maxPageSize] != "inv-200" {
		t.Errorf("got %d invoices, ids[200] = %q", len(ids), ids[min(len(ids)-1, maxPageSize)])
	}
	if got := strings.Join(pages(), " "); got != "limit=200 limit=200&offset=200" {
		t.Errorf("queries = %q", got)
	}

	// Перерваний обхід не тягне наступну сторінку.
	for range c.Invoices.All(context.Background()) {
		break
	}
	if got := len(pages()); got != 3 {
		t.Errorf("break: %d pages fetched in total, want 3", got)
	}

	c, _ = stub(t, 403, `{"code":"forbidden","message":"restricted token"}`)
	for _, err := range c.Invoices.All(context.Background()) {
		if !errors.Is(err, ErrForbidden) {
			t.Errorf("err = %v", err)
		}
	}
}

func TestWebhooks(t *testing.T) {
	ctx := context.Background()
	endpoint := `{"id":"w1","url":"https://example.com/prro-hook","events":["task.completed"],"active":true,
		"created_at":"2026-07-14T10:00:00Z","updated_at":"2026-07-14T10:00:00Z"}`

	c, reqs := stub(t, 200, `{"webhooks":[`+endpoint+`],"limit":3,"used":1}`)
	list, err := c.Webhooks.List(ctx)
	if err != nil || len(list.Webhooks) != 1 || list.Webhooks[0].Events[0] != EventTaskCompleted ||
		list.Limit != 3 || list.Used != 1 {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
	expect(t, <-reqs, http.MethodGet, "/v1/webhooks", "")

	c, reqs = stub(t, 201, strings.Replace(endpoint, `"id"`, `"secret":"s3cr3t","id"`, 1))
	created, err := c.Webhooks.Create(ctx, CreateWebhookParams{
		URL: "https://example.com/prro-hook", Events: []WebhookEvent{EventTaskCompleted},
	})
	if err != nil || created.Secret != "s3cr3t" || created.ID != "w1" {
		t.Fatalf("created = %+v, err = %v", created, err)
	}
	r := <-reqs
	expect(t, r, http.MethodPost, "/v1/webhooks", "")
	if r.Body != `{"url":"https://example.com/prro-hook","events":["task.completed"]}` {
		t.Errorf("body = %s", r.Body)
	}

	c, _ = stub(t, 409, `{"code":"webhook_limit_reached","message":"limit","details":{"limit":3,"used":3}}`)
	_, err = c.Webhooks.Create(ctx, CreateWebhookParams{URL: "https://example.com/prro-hook"})
	if apiErr, ok := errors.AsType[*Error](err); !errors.Is(err, ErrWebhookLimitReached) || !ok ||
		string(apiErr.Details) != `{"limit":3,"used":3}` {
		t.Fatalf("err = %v", err)
	}

	updates := map[string]UpdateWebhookParams{
		`{"active":false}`:                  {Active: new(false)},
		`{"events":[]}`:                     {Events: []WebhookEvent{}},
		`{"url":"https://example.com/new"}`: {URL: "https://example.com/new"},
	}
	for want, params := range updates {
		c, reqs = stub(t, 200, endpoint)
		if _, err := c.Webhooks.Update(ctx, "w1", params); err != nil {
			t.Fatal(err)
		}
		r := <-reqs
		expect(t, r, http.MethodPatch, "/v1/webhooks/w1", "")
		if r.Body != want {
			t.Errorf("update body = %s, want %s", r.Body, want)
		}
	}

	c, reqs = stub(t, http.StatusNoContent, "")
	if err := c.Webhooks.Delete(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	expect(t, <-reqs, http.MethodDelete, "/v1/webhooks/w1", "")

	c, reqs = stub(t, 200, `{"deliveries":[{"id":"d1","event":"task.failed","status":"dead","attempts":8,
		"cash_register_id":"r1","data":{"task_id":"t1","status":"failed"},"last_error":"HTTP 500",
		"created_at":"2026-07-14T10:15:04Z"}]}`)
	ds, err := c.Webhooks.Deliveries(ctx, "w1", &ListDeliveriesParams{Status: DeliveryDead, Limit: 10})
	if err != nil || len(ds) != 1 || ds[0].Status != DeliveryDead || string(ds[0].Data) != `{"task_id":"t1","status":"failed"}` {
		t.Fatalf("deliveries = %+v, err = %v", ds, err)
	}
	expect(t, <-reqs, http.MethodGet, "/v1/webhooks/w1/deliveries", "limit=10&status=dead")

	c, reqs = stub(t, 409, `{"code":"delivery_in_flight","message":"attempt in progress"}`)
	_, err = c.Webhooks.Replay(ctx, "w1", "d1")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "delivery_in_flight" {
		t.Fatalf("err = %v", err)
	}
	expect(t, <-reqs, http.MethodPost, "/v1/webhooks/w1/deliveries/d1/replay", "")
}
