package prro

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Тести контракту звіряють клієнт із testdata/openapi.yaml — копією
// контракту сервісу (оновлюється через make sync-spec): кожна операція має
// метод, кожне значення перелічення — константу, кожне поле схеми — поле
// структури, і навпаки.

// ynode — вузол YAML: скаляр, мапа або послідовність.
type ynode struct {
	val   string
	keys  []string
	m     map[string]*ynode
	items []*ynode
}

func (n *ynode) set(key, val string) *ynode {
	if n.m == nil {
		n.m = map[string]*ynode{}
	}
	child := &ynode{val: unquote(val)}
	n.keys = append(n.keys, key)
	n.m[key] = child
	return child
}

// at повертає вузол за шляхом ключів; "[]" — перший елемент послідовності.
func (n *ynode) at(path ...string) *ynode {
	for _, k := range path {
		if n == nil {
			return nil
		}
		if k == "[]" {
			if len(n.items) == 0 {
				return nil
			}
			n = n.items[0]
			continue
		}
		n = n.m[k]
	}
	return n
}

// seq повертає елементи послідовності; nil-безпечний.
func (n *ynode) seq() []*ynode {
	if n == nil {
		return nil
	}
	return n.items
}

// list повертає скаляри послідовності — блокової або [a, b].
func (n *ynode) list() []string {
	if n == nil {
		return nil
	}
	if len(n.items) > 0 {
		out := make([]string, len(n.items))
		for i, it := range n.items {
			out[i] = it.val
		}
		return out
	}
	inner, ok := strings.CutPrefix(n.val, "[")
	if !ok {
		return nil
	}
	var out []string
	for v := range strings.SplitSeq(strings.TrimSuffix(inner, "]"), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, unquote(v))
		}
	}
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// splitKey розбирає рядок "key: value" або "key:".
func splitKey(s string) (key, val string, ok bool) {
	if s != "" && (s[0] == '"' || s[0] == '\'') {
		end := strings.IndexByte(s[1:], s[0])
		if end < 0 || !strings.HasPrefix(s[end+2:], ":") {
			return "", "", false
		}
		return s[1 : end+1], strings.TrimSpace(s[end+3:]), true
	}
	if k, v, found := strings.Cut(s, ": "); found {
		return k, strings.TrimSpace(v), true
	}
	if k, found := strings.CutSuffix(s, ":"); found {
		return k, "", true
	}
	return "", "", false
}

// parseYAML розбирає підмножину YAML, якою написано контракт: мапи й
// послідовності відступами, скаляри, однорядкові flow-значення і блокові
// скаляри (">-"). Вміст скалярів-продовжень пропускається.
func parseYAML(r io.Reader) (*ynode, error) {
	type frame struct {
		indent int
		n      *ynode
	}
	root := &ynode{}
	stack := []frame{{-1, root}}
	skipDeeper := -1

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		content := strings.TrimLeft(line, " ")
		if content == "" || strings.HasPrefix(content, "#") {
			continue
		}
		indent := len(line) - len(content)
		if skipDeeper >= 0 {
			if indent > skipDeeper {
				continue
			}
			skipDeeper = -1
		}
		for stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1].n

		keyIndent := indent
		if rest, isItem := strings.CutPrefix(content, "-"); isItem && (rest == "" || rest[0] == ' ') {
			item := &ynode{}
			parent.items = append(parent.items, item)
			stack = append(stack, frame{indent, item})
			rest = strings.TrimSpace(rest)
			k, v, ok := splitKey(rest)
			if !ok {
				item.val = unquote(rest)
				continue
			}
			parent, content, keyIndent = item, k+": "+v, indent+2
			if v == "" {
				content = k + ":"
			}
		}

		key, val, ok := splitKey(content)
		if !ok {
			continue
		}
		switch child := parent.set(key, val); val {
		case "":
			stack = append(stack, frame{keyIndent, child})
		default:
			// Блоковий скаляр або багаторядковий простий скаляр: глибші
			// рядки — його вміст.
			if val == ">-" || val == ">" || val == "|" || val == "|-" {
				child.val = ""
			}
			skipDeeper = keyIndent
		}
	}
	return root, sc.Err()
}

func loadSpec(t *testing.T) *ynode {
	t.Helper()
	f, err := os.Open("testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	spec, err := parseYAML(f)
	if err != nil {
		t.Fatal(err)
	}
	if spec.at("paths") == nil || spec.at("components", "schemas") == nil {
		t.Fatal("openapi.yaml: no paths or components.schemas")
	}
	return spec
}

func TestContractVersion(t *testing.T) {
	if v := loadSpec(t).at("info", "version").val; v != APIVersion {
		t.Errorf("testdata/openapi.yaml is %s, APIVersion = %s: review the contract changes and bump APIVersion", v, APIVersion)
	}
}

// operations зіставляє operationId контракту з викликом клієнта.
// Ідентифікатори в шляху — "ID1", "ID2".
var operations = map[string]func(ctx context.Context, c *Client) error{
	"getVersion": func(ctx context.Context, c *Client) error { _, err := c.System.Version(ctx); return err },
	"whoami":     func(ctx context.Context, c *Client) error { _, err := c.System.WhoAmI(ctx); return err },
	"createReceipt": func(ctx context.Context, c *Client) error {
		_, err := c.Receipts.Create(ctx, &ServiceReceipt{CashRegisterID: "ID1", Type: ReceiptServiceIn, Sum: "1.00"})
		return err
	},
	"getTask":                   func(ctx context.Context, c *Client) error { _, err := c.Tasks.Get(ctx, "ID1"); return err },
	"listCashRegistersForToken": func(ctx context.Context, c *Client) error { _, err := c.CashRegisters.List(ctx); return err },
	"getCashRegisterState":      func(ctx context.Context, c *Client) error { _, err := c.CashRegisters.Get(ctx, "ID1"); return err },
	"openShift":                 func(ctx context.Context, c *Client) error { _, err := c.Shifts.Open(ctx, "ID1", nil); return err },
	"closeShift":                func(ctx context.Context, c *Client) error { _, err := c.Shifts.Close(ctx, "ID1"); return err },
	"closeOfflineSession": func(ctx context.Context, c *Client) error {
		_, err := c.OfflineSessions.Close(ctx, "ID1", nil)
		return err
	},
	"retryOfflineSubmission": func(ctx context.Context, c *Client) error { _, err := c.OfflineSessions.Retry(ctx, "ID1"); return err },
	"abandonOfflineSession": func(ctx context.Context, c *Client) error {
		_, err := c.OfflineSessions.Abandon(ctx, "ID1")
		return err
	},
	"clearRegisterAttention": func(ctx context.Context, c *Client) error {
		_, err := c.CashRegisters.ClearAttention(ctx, "ID1")
		return err
	},
	"setCashBalance": func(ctx context.Context, c *Client) error {
		_, err := c.CashRegisters.SetCashBalance(ctx, "ID1", "0.00")
		return err
	},
	"billingUsage": func(ctx context.Context, c *Client) error { _, err := c.Billing.Usage(ctx, nil); return err },
	"listInvoices": func(ctx context.Context, c *Client) error { _, err := c.Invoices.List(ctx, nil); return err },
	"listWebhooks": func(ctx context.Context, c *Client) error { _, err := c.Webhooks.List(ctx); return err },
	"createWebhook": func(ctx context.Context, c *Client) error {
		_, err := c.Webhooks.Create(ctx, CreateWebhookParams{URL: "https://example.com"})
		return err
	},
	"updateWebhook": func(ctx context.Context, c *Client) error {
		_, err := c.Webhooks.Update(ctx, "ID1", UpdateWebhookParams{})
		return err
	},
	"deleteWebhook": func(ctx context.Context, c *Client) error { return c.Webhooks.Delete(ctx, "ID1") },
	"listWebhookDeliveries": func(ctx context.Context, c *Client) error {
		_, err := c.Webhooks.Deliveries(ctx, "ID1", nil)
		return err
	},
	"replayWebhookDelivery": func(ctx context.Context, c *Client) error {
		_, err := c.Webhooks.Replay(ctx, "ID1", "ID2")
		return err
	},
	"systemDPSStatus": func(ctx context.Context, c *Client) error { _, err := c.System.DPS(ctx); return err },
}

func TestContractOperations(t *testing.T) {
	spec := loadSpec(t)
	paths := spec.at("paths")
	seen := map[string]bool{}

	for _, path := range paths.keys {
		for _, method := range []string{"get", "post", "put", "patch", "delete"} {
			op := paths.at(path, method)
			if op == nil {
				continue
			}
			id := op.at("operationId").val
			seen[id] = true
			call, ok := operations[id]
			if !ok {
				t.Errorf("%s %s (%s): no client method", strings.ToUpper(method), path, id)
				continue
			}

			// Шаблон шляху з ідентифікаторами ID1, ID2 по порядку.
			want, n := path, 0
			for strings.Contains(want, "{") {
				n++
				start, end := strings.Index(want, "{"), strings.Index(want, "}")
				want = want[:start] + "ID" + string(rune('0'+n)) + want[end+1:]
			}
			want = strings.ToUpper(method) + " " + want

			c, reqs := stub(t, http.StatusOK, `{}`)
			if err := call(context.Background(), c); err != nil {
				t.Errorf("%s: %v", id, err)
				continue
			}
			r := <-reqs
			if got := r.Method + " " + r.Path; got != want {
				t.Errorf("%s: client sends %s, contract says %s", id, got, want)
			}
			fiscal := slices.ContainsFunc(op.at("parameters").seq(), func(p *ynode) bool {
				return p.at("$ref") != nil && strings.HasSuffix(p.at("$ref").val, "/IdempotencyKey")
			})
			if (r.Key != "") != fiscal {
				t.Errorf("%s: Idempotency-Key sent = %v, contract requires = %v", id, r.Key != "", fiscal)
			}
		}
	}
	for id := range operations {
		if !seen[id] {
			t.Errorf("%s: client method for an operation the contract does not have", id)
		}
	}
}

func TestContractEnums(t *testing.T) {
	spec := loadSpec(t)
	schemas := spec.at("components", "schemas")
	str := func(vs ...string) []string { return vs }

	tests := []struct {
		path []string
		want []string
		// skip — значення контракту, які бібліотека свідомо не відображає.
		skip []string
	}{
		{
			path: []string{"Task", "properties", "type"},
			want: str(string(TaskReceipt), string(TaskOpenShift), string(TaskCloseShift),
				string(TaskCloseOfflineSession), string(TaskAbandonOfflineSession), string(TaskSyncRegister)),
			skip: str("verify_key", "dps_objects"), // завдання кабінету
		},
		{
			path: []string{"Task", "properties", "status"},
			want: str(string(TaskPending), string(TaskProcessing), string(TaskSucceeded), string(TaskFailed)),
		},
		{
			path: []string{"Fault", "properties", "class"},
			want: str(string(FaultResync), string(FaultConfig), string(FaultPrecursor), string(FaultUserAction),
				string(FaultTransient), string(FaultRequest), string(FaultInternal)),
		},
		{
			path: []string{"PaymentType"},
			want: str(string(PaymentCash), string(PaymentCard), string(PaymentCertificate),
				string(PaymentBankTransfer), string(PaymentDirectDebit)),
		},
		{
			path: []string{"SettlementReceiptInput", "properties", "type"},
			want: str(string(ReceiptSale), string(ReceiptReturn), string(ReceiptStorno)),
		},
		{
			path: []string{"ServiceReceiptInput", "properties", "type"},
			want: str(string(ReceiptServiceIn), string(ReceiptServiceOut)),
		},
		{
			path: []string{"AbandonedDocument", "properties", "kind"},
			want: str(string(ReceiptSale), string(ReceiptReturn), string(ReceiptStorno),
				string(ReceiptServiceIn), string(ReceiptServiceOut)),
		},
		{
			path: []string{"CashRegisterState", "properties", "mode"},
			want: str(string(RegisterProduction), string(RegisterTest)),
		},
		{
			path: []string{"CashRegisterState", "properties", "state"},
			want: str(string(RegisterClosed), string(RegisterOpened), string(RegisterOffline)),
		},
		{
			path: []string{"ShiftMode"},
			want: str(string(ShiftManual), string(ShiftRoundTheClock), string(ShiftWorkingHours)),
		},
		{
			path: []string{"CashRegisterState", "properties", "current_shift", "properties", "status"},
			want: str(string(ShiftOpening), string(ShiftOpened), string(ShiftClosing), string(ShiftClosed)),
		},
		{
			path: []string{"CashRegisterState", "properties", "attention", "properties", "reason"},
			want: str(string(AttentionRecurringRemediation), string(AttentionOfflineSubmissionPaused),
				string(AttentionAutoCloseFailing)),
		},
		{
			path: []string{"WebhookEvent"},
			want: str(string(EventTaskCompleted), string(EventTaskFailed), string(EventShiftOpened),
				string(EventShiftClosed), string(EventKeyExpiring), string(EventRegisterOffline),
				string(EventRegisterOnline), string(EventRegisterOfflineLimit), string(EventRegisterOfflineAbandoned),
				string(EventRegisterRemediated), string(EventRegisterNeedsAttention), string(EventClientBalanceLow)),
		},
		{
			path: []string{"WebhookDelivery", "properties", "status"},
			want: str(string(DeliveryPending), string(DeliveryDelivering), string(DeliveryDelivered),
				string(DeliveryFailed), string(DeliveryDead)),
		},
		{
			path: []string{"Invoice", "properties", "status"},
			want: str(string(InvoiceDraft), string(InvoiceIssued), string(InvoicePaid), string(InvoiceOverdue)),
		},
		{
			path: []string{"DPSPulse", "properties", "verdict"},
			want: str(string(DPSOnline), string(DPSDegraded), string(DPSOffline)),
		},
	}
	for _, tt := range tests {
		name := strings.Join(tt.path, ".")
		node := schemas.at(tt.path...)
		if node == nil {
			t.Errorf("%s: not in contract", name)
			continue
		}
		got := slices.DeleteFunc(node.at("enum").list(), func(v string) bool { return slices.Contains(tt.skip, v) })
		slices.Sort(got)
		slices.Sort(tt.want)
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: contract enum %v, Go constants %v", name, got, tt.want)
		}
	}
}

// jsonFields повертає імена json-полів структури.
func jsonFields(typ reflect.Type) []string {
	var out []string
	for f := range typ.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}

func TestContractSchemas(t *testing.T) {
	spec := loadSpec(t)
	schema := func(path ...string) []string { return append([]string{"components", "schemas"}, path...) }
	body := func(path, method string) []string {
		return []string{"paths", path, method, "requestBody", "content", "application/json", "schema"}
	}

	tests := []struct {
		path []string
		typ  any
		// skip — поля контракту, які бібліотека свідомо не відображає.
		skip []string
	}{
		{schema("CashRegisterListItem"), CashRegister{}, nil},
		{schema("CashRegisterState"), CashRegisterState{}, nil},
		{schema("CashRegisterState", "properties", "current_shift"), Shift{}, nil},
		{schema("CashRegisterState", "properties", "offline_session"), OfflineSession{}, nil},
		{schema("CashRegisterState", "properties", "offline_limits"), OfflineLimits{}, nil},
		{schema("CashRegisterState", "properties", "attention"), Attention{}, nil},
		{schema("CashBalance"), CashBalance{}, nil},
		{schema("ShiftTotals"), ShiftTotals{}, nil},
		{schema("ShiftTotalsSide"), ShiftTotalsSide{}, nil},
		{schema("ShiftTotalsSide", "properties", "pay_forms", "items"), PayFormTotal{}, nil},
		{schema("CashBalanceDeclaration"), CashBalanceDeclaration{}, nil},
		{schema("Task"), Task{}, nil},
		{schema("Fault"), Fault{}, nil},
		// Результати завдань кабінету (перевірка ключа, об'єкти ДПС)
		// машинному API не потрібні.
		{schema("TaskResult"), TaskResult{}, []string{"key_verification", "key_password_check", "dps_objects"}},
		{schema("DocResult"), DocResult{}, nil},
		{schema("ReturnOriginal"), ReturnOriginal{}, nil},
		{schema("OfflineAbandonResult"), OfflineAbandonResult{}, nil},
		{schema("AbandonedDocument"), AbandonedDocument{}, nil},
		{schema("ReconcileReport"), ReconcileReport{}, nil},
		{schema("ReconcileReport", "properties", "cash"), ReconcileCash{}, nil},
		{schema("ReconcileReport", "properties", "totals"), ReconcileTotals{}, nil},
		{schema("ShiftTotalsBrief"), ShiftTotalsBrief{}, nil},
		// mode передається параметром запиту (WithAsync), а не полем тіла.
		{schema("SettlementReceiptInput"), SettlementReceipt{}, []string{"mode"}},
		{schema("SettlementReceiptInput", "properties", "original"), ReceiptOriginal{}, nil},
		{schema("ServiceReceiptInput"), ServiceReceipt{}, []string{"mode"}},
		{schema("ReceiptItem"), ReceiptItem{}, nil},
		{schema("ReceiptItem", "properties", "discount"), Discount{}, nil},
		{schema("ReceiptPayment"), Payment{}, nil},
		{schema("ReceiptPayment", "properties", "card"), CardDetails{}, nil},
		{schema("WebhookEndpoint"), WebhookEndpoint{}, nil},
		{schema("WebhookDelivery"), WebhookDelivery{}, nil},
		{schema("BillingUsage"), BillingUsage{}, nil},
		{schema("BillingUsage", "properties", "period"), BillingPeriod{}, nil},
		{schema("BillingUsage", "properties", "tariff"), Tariff{}, nil},
		{schema("Invoice"), Invoice{}, nil},
		{schema("DPSPulse"), DPSStatus{}, nil},
		{schema("DPSPulse", "properties", "tunables"), DPSTunables{}, nil},
		{[]string{"paths", "/v1/whoami", "get", "responses", "200", "content", "application/json", "schema"}, Identity{}, nil},
		{body("/v1/cash-registers/{id}/shifts", "post"), OpenShiftParams{}, []string{"mode"}},
		{body("/v1/cash-registers/{id}/offline-session/close", "post"), CloseOfflineSessionParams{}, []string{"mode"}},
		{body("/v1/webhooks", "post"), CreateWebhookParams{}, nil},
		{body("/v1/webhooks/{id}", "patch"), UpdateWebhookParams{}, nil},
	}
	for _, tt := range tests {
		typ := reflect.TypeOf(tt.typ)
		node := spec.at(tt.path...)
		if node == nil || node.at("properties") == nil {
			t.Errorf("%s: no such schema with properties in contract", typ.Name())
			continue
		}
		props := slices.DeleteFunc(slices.Clone(node.at("properties").keys), func(p string) bool {
			return slices.Contains(tt.skip, p)
		})
		fields := jsonFields(typ)
		for _, p := range props {
			if !slices.Contains(fields, p) {
				t.Errorf("%s: contract field %q has no struct field", typ.Name(), p)
			}
		}
		for _, f := range fields {
			if !slices.Contains(props, f) {
				t.Errorf("%s: struct field %q is not in the contract", typ.Name(), f)
			}
		}
	}
}

func TestParseYAML(t *testing.T) {
	doc := `# comment
a:
  b: plain
  c: >-
    folded text
    : not a key
  d: "quoted: value"
  "200": { $ref: "#/x" }
  e: [one, "two", three]
  f:
    - x
    - y
  g:
    - name: n1
      in: path
    - $ref: "#/p"
h: 1
`
	root, err := parseYAML(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		"a.b": "plain", "a.c": "", "a.d": "quoted: value", "a.200": `{ $ref: "#/x" }`, "h": "1",
		"a.g.[].name": "n1", "a.g.[].in": "path",
	}
	for path, want := range checks {
		n := root.at(strings.Split(path, ".")...)
		if n == nil || n.val != want {
			t.Errorf("%s = %+v, want %q", path, n, want)
		}
	}
	if got := root.at("a", "e").list(); !slices.Equal(got, []string{"one", "two", "three"}) {
		t.Errorf("flow list = %q", got)
	}
	if got := root.at("a", "f").list(); !slices.Equal(got, []string{"x", "y"}) {
		t.Errorf("block list = %q", got)
	}
	if got := root.at("a").keys; !slices.Equal(got, []string{"b", "c", "d", "200", "e", "f", "g"}) {
		t.Errorf("keys = %q", got)
	}
	if g := root.at("a", "g"); len(g.items) != 2 || g.items[1].at("$ref").val != "#/p" {
		t.Errorf("sequence of maps = %+v", g)
	}
}
