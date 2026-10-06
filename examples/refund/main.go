// Повернення: чек повернення посилається на початковий чек продажу. Якщо
// той видано в PRRO.cloud, сервіс звіряє суму повернень із сумою продажу.
//
//	PRRO_TOKEN=… PRRO_REGISTER_ID=… PRRO_ORIGINAL_FN=… go run ./examples/refund
package main

import (
	"context"
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
	c := prro.NewClient(os.Getenv("PRRO_TOKEN"), prro.WithBaseURL(os.Getenv("PRRO_BASE_URL")))

	task, err := c.Receipts.Create(ctx, &prro.SettlementReceipt{
		CashRegisterID: os.Getenv("PRRO_REGISTER_ID"),
		Type:           prro.ReceiptReturn,
		Original:       &prro.ReceiptOriginal{FiscalNumber: os.Getenv("PRRO_ORIGINAL_FN")},
		Comment:        "Повернення за замовленням 10412",
		Items: []prro.ReceiptItem{
			{Name: "Кава американо", Quantity: "1", Price: "65.00", TaxLetters: "А"},
		},
		Payments: []prro.Payment{{Type: prro.PaymentCard, Sum: "65.00"}},
	}, prro.WithIdempotencyKey("refund-10412"))
	if err != nil {
		// Повернення понад суму продажу або сторнований початковий чек —
		// ErrTaskFailed із Fault.Code у просторі return.*.
		return err
	}
	if !task.Done() {
		if task, err = c.Tasks.Wait(ctx, task.ID); err != nil {
			return err
		}
	}

	r := task.Result.Receipt
	fmt.Println("чек повернення:", r.FiscalNumber, r.ReceiptURL)
	if r.Original != nil && !r.Original.Verified {
		fmt.Println("початковий чек видано поза PRRO.cloud — повернення прийнято без звірки")
	}
	return nil
}
