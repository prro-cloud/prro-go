# Зміни

Формат — [Keep a Changelog](https://keepachangelog.com/uk/1.1.0/), версії — за
[семантичним версіонуванням](https://semver.org/lang/uk/). До 1.0.0 мінорна
версія може містити несумісні зміни API.

## [Unreleased]

Контракт API 0.6.0.

### Змінено

- **Несумісно:** `Webhooks.List` повертає `*WebhookList` — перелік вебхуків
  разом із лімітом: `Limit` (кас у клієнта плюс один) і `Used` (зареєстровано,
  включно з призупиненими). Було: `[]WebhookEndpoint` — тепер `list.Webhooks`.

### Додано

- Подія `receipt.registered` (`prro.EventReceiptRegistered`) — повний вміст
  кожного зареєстрованого чека, і з API, і з панелі; дані — `webhook.ReceiptData`
  через `e.Receipt()`, джерело — `webhook.SourceAPI` / `webhook.SourceCabinet`.
- `prro.ErrWebhookLimitReached` — 409 `webhook_limit_reached` у
  `Webhooks.Create`, коли ліміт вебхуків вичерпано; limit і used — у
  `Error.Details`.
- Тест контракту звіряє обгортку відповіді `GET /v1/webhooks` і дані
  `receipt.registered` (`ReceiptRegisteredData`).

### Документація

- Строки зберігання історії доставок: доставлені — 30 днів, невдалі — 14 днів
  від останньої спроби; `Webhooks.Replay` доступний, поки доставка є в історії.

## [0.1.1] — 2026-10-07

### Додано

- `webhook.Event.Sequence` і `WebhookDelivery.Sequence` — номер події в межах
  каси, за яким упорядковують події: невдала доставка більше не затримує
  наступні події каси, тож після повтору чи replay вона може прийти пізніше
  за них.

## [0.1.0] — 2026-10-06

### Додано

- Клієнт машинного API PRRO.cloud (контракт 0.5.0): чеки, завдання, каси,
  зміни, офлайн-сесії, білінг, рахунки, вебхуки, службові маршрути.
- Автоматичний ключ ідемпотентності (UUIDv7) і безпечні повтори з
  експоненційною затримкою та `Retry-After`.
- Типізовані помилки `*prro.Error` з маркерами для `errors.Is` і описом
  збою завдання `Fault`.
- `Tasks.Wait` — очікування завершення завдання.
- `Invoices.All` — обхід усіх рахунків через `iter.Seq2`.
- Підпакет `webhook`: перевірка підпису, типізовані дані подій,
  `http.Handler`.
- Тести відповідності контракту OpenAPI.

[Unreleased]: https://github.com/prro-cloud/prro-go/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/prro-cloud/prro-go/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/prro-cloud/prro-go/releases/tag/v0.1.0
