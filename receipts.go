package prro

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ReceiptType — вид розрахункового документа.
type ReceiptType string

// Види розрахункових документів.
const (
	ReceiptSale       ReceiptType = "sale"        // реалізація
	ReceiptReturn     ReceiptType = "return"      // повернення
	ReceiptStorno     ReceiptType = "storno"      // сторно
	ReceiptServiceIn  ReceiptType = "service_in"  // службове внесення
	ReceiptServiceOut ReceiptType = "service_out" // службова видача (інкасація)
)

// PaymentType — форма оплати. Фіскальний код і назву, які потраплять у чек,
// визначає сервіс; залишок готівки каси рухає лише [PaymentCash].
type PaymentType string

// Форми оплати.
const (
	PaymentCash         PaymentType = "cash"          // 0 ГОТІВКА
	PaymentCard         PaymentType = "card"          // 301 КАРТКА
	PaymentCertificate  PaymentType = "certificate"   // 305 СЕРТИФІКАТ
	PaymentBankTransfer PaymentType = "bank_transfer" // 306 ПЕРЕКАЗ З ПОТОЧНОГО РАХУНКУ
	PaymentDirectDebit  PaymentType = "direct_debit"  // 100000 ПРЯМИЙ ДЕБЕТ
)

// ReceiptInput — документ для [ReceiptsService.Create]: [*SettlementReceipt]
// (розрахунок за товари) або [*ServiceReceipt] (рух готівки). Інших
// реалізацій немає — поля чужої форми API відхиляє з 422, тож форми
// розділено на рівні типів.
type ReceiptInput interface {
	validateReceipt() error
}

// SettlementReceipt — розрахунок за товари: реалізація, повернення або
// сторно.
type SettlementReceipt struct {
	// CashRegisterID — каса, на якій реєструють документ; обов'язкове.
	CashRegisterID string `json:"cash_register_id"`
	// Type — [ReceiptSale], [ReceiptReturn] або [ReceiptStorno].
	Type ReceiptType `json:"type"`
	// Cashier — ім'я касира, яке друкують у чеку; без нього рядка не буде.
	Cashier string `json:"cashier,omitempty"`
	// Comment — вільна примітка, яку друкують на чеку, — наприклад номер
	// замовлення.
	Comment string `json:"comment,omitempty"`
	// SoldAt — момент фактичного продажу, якщо документ дійшов до сервісу
	// пізніше за касу. Друкується рядком «Продаж від 03.09.2026 18:11», але
	// фіскальним часом документа не стає. Час у майбутньому або старіший за
	// 31 день — 422.
	SoldAt time.Time `json:"sold_at,omitzero"`
	// Original — початковий чек; обов'язковий для повернення та сторно.
	Original *ReceiptOriginal `json:"original,omitempty"`
	// Items — позиції чека; щонайменше одна.
	Items []ReceiptItem `json:"items"`
	// Payments — оплати чека; щонайменше одна.
	Payments []Payment `json:"payments"`
	// RoundingStep — крок заокруглення готівки ("0.10" за правилами НБУ);
	// порожній — без заокруглення.
	RoundingStep Amount `json:"rounding_step,omitempty"`
}

func (r *SettlementReceipt) validateReceipt() error {
	if r == nil {
		return errNilReceipt
	}
	switch r.Type {
	case ReceiptSale, ReceiptReturn, ReceiptStorno:
		return nil
	}
	return fmt.Errorf("prro: SettlementReceipt: type must be sale, return or storno, got %q", r.Type)
}

// ReceiptOriginal — посилання на початковий чек повернення або сторно.
type ReceiptOriginal struct {
	// FiscalNumber — фіскальний номер початкового чека.
	FiscalNumber string `json:"fiscal_number"`
	// RegisterFiscalNumber — фіскальний номер каси початкового чека, якщо
	// це інша каса; без нього чек шукають на касі повернення.
	RegisterFiscalNumber string `json:"register_fiscal_number,omitempty"`
	// Date — дата початкового чека.
	Date Date `json:"date,omitempty"`
}

// ReceiptItem — позиція чека.
type ReceiptItem struct {
	// Name — назва позиції, як її друкують у чеку.
	Name string `json:"name"`
	// Quantity — кількість десятковим рядком, до 3 знаків після коми:
	// "1", "0.750".
	Quantity string `json:"quantity"`
	// Price — ціна за одиницю.
	Price Amount `json:"price"`
	// TaxLetters — податкові літери каси, напр. "А" або "АГ". Порожнє
	// значення застосовує типову літеру каси, "-" робить позицію
	// неоподаткованою.
	TaxLetters string `json:"tax_letters,omitempty"`
	// Code — внутрішній код товару.
	Code string `json:"code,omitempty"`
	// Barcode — штрихкод товару.
	Barcode string `json:"barcode,omitempty"`
	// UKTZED — код УКТЗЕД; взаємовиключний з DKPP.
	UKTZED string `json:"uktzed,omitempty"`
	// DKPP — код ДКПП; взаємовиключний з UKTZED.
	DKPP string `json:"dkpp,omitempty"`
	// UnitCode — код одиниці виміру (UNITCD).
	UnitCode int `json:"unit_code,omitempty"`
	// UnitName — назва одиниці виміру: «шт», «кг».
	UnitName string `json:"unit_name,omitempty"`
	// Discount — знижка на позицію.
	Discount *Discount `json:"discount,omitempty"`
	// ExciseLabels — коди марок акцизного податку.
	ExciseLabels []string `json:"excise_labels,omitempty"`
	// Comment — примітка до позиції.
	Comment string `json:"comment,omitempty"`
}

// Discount — знижка на позицію.
type Discount struct {
	// Sum — сума знижки; вказується і для відсоткової знижки.
	Sum Amount `json:"sum"`
	// Percent — позначає знижку як відсоткову: "10".
	Percent string `json:"percent,omitempty"`
}

// Payment — одна оплата чека.
type Payment struct {
	// Type — форма оплати.
	Type PaymentType `json:"type"`
	// Sum — сума, віднесена на цю форму оплати.
	Sum Amount `json:"sum"`
	// Provided — отримано готівкою; решта = Provided − Sum.
	Provided Amount `json:"provided,omitempty"`
	// Means — засіб оплати, який друкується замість назви форми:
	// «ПЕРЕКАЗ З КАРТКИ», «ТАЛОН», «Подарунковий сертифікат». Фіскальний
	// код форми лишається за Type. Для готівки не приймається.
	Means string `json:"means,omitempty"`
	// Card — реквізити еквайрингу для оплати карткою.
	Card *CardDetails `json:"card,omitempty"`
}

// CardDetails — реквізити еквайрингу. Поля Acquirer* описують еквайра
// торговця, а не самого торговця.
type CardDetails struct {
	SystemName    string `json:"system_name,omitempty"`     // назва платіжної системи
	AcquirerID    string `json:"acquirer_id,omitempty"`     // ідентифікатор еквайра
	AcquirerTaxID string `json:"acquirer_tax_id,omitempty"` // податковий номер еквайра
	AcquirerName  string `json:"acquirer_name,omitempty"`   // найменування еквайра
	// TransactionDate — дата й час транзакції на платіжному пристрої.
	TransactionDate time.Time `json:"transaction_date,omitzero"`
	// TransactionNumber — номер транзакції на платіжному пристрої.
	TransactionNumber string `json:"transaction_number,omitempty"`
	DeviceID          string `json:"device_id,omitempty"`   // ідентифікатор платіжного пристрою
	EPZDetails        string `json:"epz_details,omitempty"` // замаскований номер картки
	AuthCode          string `json:"auth_code,omitempty"`   // код авторизації
	// Commission — комісія еквайра, яку платить платник; у звичайному
	// продажу лишається порожньою.
	Commission Amount `json:"commission,omitempty"`
}

// ServiceReceipt — рух готівки повз продаж: службове внесення
// ([ReceiptServiceIn]) або інкасація ([ReceiptServiceOut]). Позицій і оплат
// не має — лише суму.
type ServiceReceipt struct {
	// CashRegisterID — каса, на якій реєструють документ; обов'язкове.
	CashRegisterID string `json:"cash_register_id"`
	// Type — [ReceiptServiceIn] або [ReceiptServiceOut].
	Type ReceiptType `json:"type"`
	// Sum — сума руху, додатна.
	Sum Amount `json:"sum"`
	// Cashier — ім'я касира, яке друкують у документі.
	Cashier string `json:"cashier,omitempty"`
	// Comment — за що гроші: «Інкасація», «Розмінна монета».
	Comment string `json:"comment,omitempty"`
	// SoldAt — момент фактичної операції, якщо документ дійшов до сервісу
	// пізніше; правила — як у [SettlementReceipt.SoldAt].
	SoldAt time.Time `json:"sold_at,omitzero"`
}

func (r *ServiceReceipt) validateReceipt() error {
	if r == nil {
		return errNilReceipt
	}
	switch r.Type {
	case ReceiptServiceIn, ReceiptServiceOut:
		return nil
	}
	return fmt.Errorf("prro: ServiceReceipt: type must be service_in or service_out, got %q", r.Type)
}

// ReceiptsService — реєстрація розрахункових документів.
type ReceiptsService service

var errNilReceipt = errors.New("prro: nil receipt")

// Create реєструє розрахунковий документ: сервіс видає локальний номер,
// підписує документ КЕП і надсилає до ДПС.
//
// Синхронно (типово) виклик чекає на результат до 30 секунд: успіх — це
// завдання зі Status == [TaskSucceeded], а фіскальний номер і посилання для
// покупця — у Result.Receipt. Якщо ДПС не встигла, повертається незавершене
// завдання — дочекайтеся його через [TasksService.Wait]. З [WithAsync]
// незавершене завдання повертається одразу.
//
// Передавайте [WithIdempotencyKey] з ключем, похідним від замовлення:
// повтор із тим самим ключем поверне те саме завдання, а не другий чек.
func (s *ReceiptsService) Create(ctx context.Context, in ReceiptInput, opts ...CallOption) (*Task, error) {
	if in == nil {
		return nil, errNilReceipt
	}
	if err := in.validateReceipt(); err != nil {
		return nil, err
	}
	return call[Task](ctx, s.client, request{
		method: http.MethodPost, path: "/v1/receipts", body: in, fiscal: true,
	}, opts...)
}
