// Швидкий старт: перший чек на касі в режимі test — безкоштовно і без
// реєстрації в податковій.
//
//	PRRO_TOKEN=… go run ./examples/quickstart
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/prro-cloud/prro-go"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	err := run(ctx)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	c := prro.NewClient(os.Getenv("PRRO_TOKEN"), prro.WithBaseURL(os.Getenv("PRRO_BASE_URL")), prro.WithUserAgent("prro-quickstart/1.0"))

	// 1. Ідентифікатор каси — разовий виклик; збережіть id у налаштуваннях.
	registers, err := c.CashRegisters.List(ctx)
	if err != nil {
		return err
	}
	if len(registers) == 0 {
		return errors.New("токен не бачить жодної каси")
	}
	reg := registers[0]
	fmt.Printf("каса %s (ФН %s), режим %s\n", reg.ID, reg.FiscalNumber, reg.Mode)

	// 2. Зміна: у режимі manual її відкривають явно, інакше перший чек
	// відкриє її сам.
	state, err := c.CashRegisters.Get(ctx, reg.ID)
	if err != nil {
		return err
	}
	if state.ShiftMode == prro.ShiftManual && state.CurrentShift == nil {
		if _, err := c.Shifts.Open(ctx, reg.ID, &prro.OpenShiftParams{Cashier: "Каса 1"}); err != nil {
			return err
		}
	}

	// 3. Чек. Ключ ідемпотентності — від замовлення: повтор не створить
	// другого чека.
	task, err := c.Receipts.Create(ctx, &prro.SettlementReceipt{
		CashRegisterID: reg.ID,
		Type:           prro.ReceiptSale,
		Items: []prro.ReceiptItem{
			{Name: "Кава американо", Quantity: "1", Price: "65.00", TaxLetters: "А"},
		},
		Payments: []prro.Payment{{Type: prro.PaymentCard, Sum: "65.00"}},
	}, prro.WithIdempotencyKey("quickstart-order-1"))
	switch {
	case errors.Is(err, prro.ErrPaymentRequired):
		return errors.New("баланс вичерпано — поповніть рахунок у кабінеті")
	case errors.Is(err, prro.ErrTaskFailed):
		// Fault.Code — стабільний код для обробки, Fault.Message — текст
		// для людини.
		if e, ok := errors.AsType[*prro.Error](err); ok && e.Fault != nil {
			return fmt.Errorf("чек не зареєстровано: %s (%s)", e.Fault.Message, e.Fault.Code)
		}
		return err
	case err != nil:
		return err
	}

	// ДПС не встигла за 30 секунд — дочікуємося завдання.
	if !task.Done() {
		if task, err = c.Tasks.Wait(ctx, task.ID); err != nil {
			return err
		}
	}

	r := task.Result.Receipt
	fmt.Println("фіскальний номер:", r.FiscalNumber)
	fmt.Println("чек для покупця: ", r.ReceiptURL)
	fmt.Println("перевірка в ДПС: ", r.TaxURL)
	return nil
}
