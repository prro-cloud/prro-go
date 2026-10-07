# prro-go

[![Go Reference](https://pkg.go.dev/badge/github.com/prro-cloud/prro-go.svg)](https://pkg.go.dev/github.com/prro-cloud/prro-go)
[![CI](https://github.com/prro-cloud/prro-go/actions/workflows/ci.yml/badge.svg)](https://github.com/prro-cloud/prro-go/actions/workflows/ci.yml)

Go-клієнт для API [PRRO.cloud](https://prro.cloud): фіскалізація чеків
програмного РРО, зміни, офлайн-сесії, білінг і вебхуки.

- Без залежностей — лише стандартна бібліотека Go.
- Безпечні повтори: ключ ідемпотентності генерується автоматично, мережеві
  збої та 5xx повторюються з експоненційною затримкою.
- Типізовані помилки для `errors.Is` і структурований опис збою ДПС.
- Підпакет [`webhook`](https://pkg.go.dev/github.com/prro-cloud/prro-go/webhook):
  перевірка підпису, типізовані події, готовий `http.Handler`.
- Звірено з контрактом OpenAPI сервісу тестами.

## Встановлення

```sh
go get github.com/prro-cloud/prro-go
```

Потрібен Go 1.27 або новіший.

## Швидкий старт

Токен випускають у [кабінеті](https://app.prro.cloud/) у розділі токенів. На
касі в режимі `test` чеки безкоштовні й не потребують реєстрації в податковій.

```go
c := prro.NewClient(os.Getenv("PRRO_TOKEN"))

task, err := c.Receipts.Create(ctx, &prro.SettlementReceipt{
	CashRegisterID: registerID, // з c.CashRegisters.List
	Type:           prro.ReceiptSale,
	Items: []prro.ReceiptItem{
		{Name: "Кава американо", Quantity: "1", Price: "65.00", TaxLetters: "А"},
	},
	Payments: []prro.Payment{{Type: prro.PaymentCard, Sum: "65.00"}},
}, prro.WithIdempotencyKey("order-10412"))
if err != nil {
	return err
}
if !task.Done() { // ДПС не відповіла за 30 секунд
	if task, err = c.Tasks.Wait(ctx, task.ID); err != nil {
		return err
	}
}

r := task.Result.Receipt
fmt.Println(r.FiscalNumber) // фіскальний номер від ДПС
fmt.Println(r.ReceiptURL)   // чек для покупця: https://r.prro.cloud/…
fmt.Println(r.TaxURL)       // той самий чек у кабінеті ДПС
```

Повний цикл — каса, зміна, чек — у [examples/quickstart](examples/quickstart/main.go).

## Ключові механіки

### Ідемпотентність

Кожна фіскальна операція (чек, зміна, офлайн-сесія) несе ключ
ідемпотентності: повтор із тим самим ключем повертає початкове завдання, а не
реєструє другий документ. Передавайте ключ, похідний від замовлення, —
тоді повтор безпечний і після перезапуску застосунку:

```go
c.Receipts.Create(ctx, receipt, prro.WithIdempotencyKey("order-10412"))
```

Без `WithIdempotencyKey` клієнт генерує UUIDv7 сам — його вистачає для
повторів у межах одного виклику. Той самий ключ з іншою операцією —
`prro.ErrIdempotencyKeyReused`.

### Синхронний і асинхронний режим

Типово виклик чекає на відповідь ДПС до 30 секунд. Не дочекався — повертає
незавершене завдання (`task.Done() == false`), яке дочікуються через
`c.Tasks.Wait`. З `prro.WithAsync()` завдання повертається одразу, а
результат приходить опитуванням або [вебхуком](#вебхуки).

### Гроші

Суми — десяткові рядки, ніколи не `float64`. Два типи, щоб їх не сплутати:

| Тип | Одиниця | Де | Приклад |
|---|---|---|---|
| `prro.Amount` | гривні | чеки, готівка, підсумки зміни | `"259.90"` |
| `prro.Kopecks` | копійки, до 4 знаків | білінг: баланс, ціна чека, рахунки | `"50.0000"` |

Рядковий літерал присвоюється напряму (`Price: "65.00"`), обчислені суми
перетворює `prro.AmountFromKop(6500)`, назад — `amount.Kop()`.

### Помилки

Відповідь API з помилкою — `*prro.Error` зі статусом, машинним кодом і
текстом. Типові випадки перевіряють через `errors.Is`:

| Помилка | Статус | Що означає |
|---|---|---|
| `ErrUnauthorized` | 401 | токен недійсний, протермінований чи відкликаний |
| `ErrForbidden` | 403 | обмежений токен звернувся до білінгу чи вебхуків |
| `ErrNotFound` | 404 | ресурсу немає або токен не має до нього доступу |
| `ErrPaymentRequired` | 402 | баланс вичерпано — нові чеки заблоковано |
| `ErrIdempotencyKeyReused` | 409 | ключ уже використано для іншої операції |
| `ErrValidation` | 422 | запит не пройшов валідацію, подробиці в `Details` |
| `ErrTaskFailed` | 422 | ДПС відхилила документ або немає відкритої зміни |

```go
switch {
case errors.Is(err, prro.ErrPaymentRequired):
	// поповнити баланс
case errors.Is(err, prro.ErrTaskFailed):
	if e, ok := errors.AsType[*prro.Error](err); ok && e.Fault != nil {
		log.Println(e.Fault.Code, e.Fault.Class) // напр. dps.check_local_number_invalid, resync
	}
}
```

`Fault.Class` підказує, що робити далі: `transient` — повторити, `request` —
виправити запит, `user_action` — потрібне рішення людини тощо.

### Чек для покупця

`ReceiptURL` — копія чека, що відкривається без авторизації: надішліть її
листом, у месенджер або покажіть QR-кодом. Параметр `?format=png` дає чек
завширшки 576 px для термопринтера 80 мм, `?format=qr` — QR-код посилання.
`TaxURL` — той самий документ у кабінеті ДПС, незалежне підтвердження
фіскалізації.

## Вебхуки

Вебхук — адреса на вашому сервері, куди PRRO.cloud сам надсилає події:
чек зареєстровано, зміну закрито, каса перейшла в офлайн, баланс добігає
кінця. Опитувати завдання тоді не потрібно.

### 1. Зареєструйте вебхук і збережіть секрет

**Секрет** — рядок, яким сервіс підписує кожну доставку: HMAC-SHA256 тіла
запиту в заголовку `X-Signature`. Ваш приймач перевіряє підпис тим самим
секретом і так відрізняє справжні доставки від підроблених. Секрет
видається **рівно один раз** — під час створення вебхука; отримати його
повторно неможливо. Збережіть його одразу у сховищі секретів (змінна
оточення, Vault тощо); якщо втратили — створіть вебхук заново.

Створити вебхук можна двома способами:

- **у кабінеті** — розділ «Webhooks» → «Додати webhook»: секрет буде показано
  після створення;
- **з коду**:

```go
wh, err := c.Webhooks.Create(ctx, prro.CreateWebhookParams{
	URL:    "https://shop.example.com/prro-hook",
	Events: []prro.WebhookEvent{prro.EventTaskCompleted, prro.EventTaskFailed}, // порожньо — усі події
})
if err != nil {
	return err
}
// wh.Secret — збережіть, наприклад, у змінну оточення PRRO_WEBHOOK_SECRET.
```

Власний секрет можна задати полем `Secret`; якщо його не вказати, секрет
згенерує сервер.

### 2. Приймайте доставки

```go
import "github.com/prro-cloud/prro-go/webhook"

secret := os.Getenv("PRRO_WEBHOOK_SECRET") // секрет із кроку 1

http.Handle("POST /prro-hook", webhook.Handler(secret, func(ctx context.Context, e *webhook.Event) error {
	if e.Type == prro.EventTaskCompleted {
		d, err := e.Task()
		if err != nil {
			return err
		}
		return markOrder(ctx, d.TaskID, d.Status)
	}
	return nil
}))
```

`Handler` перевіряє підпис до будь-якої обробки тіла і відповідає 401, якщо
підпис не збігся. Помилка вашого обробника — відповідь 500, і сервіс повторить
доставку. Доставка гарантована «щонайменше один раз»: відкидайте повтори за
`e.DeliveryID`. Подію, яку приймач не прийняв, сервіс повторить пізніше, і вона
може прийти після наступних подій тієї самої каси — упорядковуйте їх за
`e.Sequence`. Перевірити приймач можна кнопкою «Тестова відправка» в
кабінеті — прийде подія `webhook.ping`. Приклад із дедуплікацією —
[examples/webhook](examples/webhook/main.go).

## Налаштування клієнта

| Опція | Типово | Призначення |
|---|---|---|
| `WithBaseURL(url)` | `https://api.prro.cloud` | інша адреса API |
| `WithHTTPClient(hc)` | тайм-аут 60 с | власний транспорт, проксі |
| `WithRetry(n)` | 2 | кількість повторів; 0 вимикає |
| `WithLanguage("en")` | `uk` | мова повідомлень про помилки |
| `WithUserAgent("shop/1.0")` | — | назва вашого застосунку в User-Agent |
| `WithLogger(slog.Default())` | вимкнено | журнал запитів на рівні Debug |

Повторюються лише запити, які безпечно повторити: `GET`, `PUT` і фіскальні
операції з ключем ідемпотентності.

## Методи

| Сервіс | Методи |
|---|---|
| `c.Receipts` | `Create` |
| `c.Tasks` | `Get`, `Wait` |
| `c.CashRegisters` | `List`, `Get`, `ClearAttention`, `SetCashBalance` |
| `c.Shifts` | `Open`, `Close` |
| `c.OfflineSessions` | `Close`, `Retry`, `Abandon` |
| `c.Billing` | `Usage` |
| `c.Invoices` | `List`, `All` |
| `c.Webhooks` | `List`, `Create`, `Update`, `Delete`, `Deliveries`, `Replay` |
| `c.System` | `Version`, `WhoAmI`, `DPS` |

Опис кожного методу — на [pkg.go.dev](https://pkg.go.dev/github.com/prro-cloud/prro-go).
Ще приклади: [асинхронний режим](examples/async/main.go),
[повернення](examples/refund/main.go).

## Документація API

- [prro.cloud/api](https://prro.cloud/api/) — документація REST API.
- [llms-full.txt](https://prro.cloud/llms-full.txt) — уся документація одним
  файлом для ШІ-асистента.
- [openapi.yaml](https://prro.cloud/api/openapi.yaml) — контракт OpenAPI 3.1.

Бібліотека відповідає контракту версії `prro.APIVersion`.

## Розробка

```sh
make test        # тести з -race
make lint        # golangci-lint
make sync-spec   # оновити testdata/openapi.yaml з репозиторію сервісу
```

Після `make sync-spec` тести контракту показують, яких методів, полів чи
констант бракує клієнту. Зміни — у [CHANGELOG.md](CHANGELOG.md), вразливості —
за [SECURITY.md](SECURITY.md).

## Ліцензія

[Apache 2.0](LICENSE)
