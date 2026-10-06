// Асинхронний режим: виклик повертає завдання одразу, не тримаючи з'єднання
// до відповіді ДПС, а результат забирають опитуванням (або вебхуком —
// див. examples/webhook).
//
//	PRRO_TOKEN=… PRRO_REGISTER_ID=… go run ./examples/async
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
	// Межу очікування результату задає контекст.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	err := run(ctx)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	c := prro.NewClient(os.Getenv("PRRO_TOKEN"), prro.WithBaseURL(os.Getenv("PRRO_BASE_URL")))

	task, err := c.Receipts.Create(ctx, &prro.ServiceReceipt{
		CashRegisterID: os.Getenv("PRRO_REGISTER_ID"),
		Type:           prro.ReceiptServiceIn,
		Sum:            prro.AmountFromKop(50000), // "500.00"
		Comment:        "Розмінна монета",
	}, prro.WithAsync(), prro.WithIdempotencyKey("float-"+time.Now().Format(time.DateOnly)))
	if err != nil {
		return err
	}
	fmt.Printf("завдання %s: %s\n", task.ID, task.Status)

	done, err := c.Tasks.Wait(ctx, task.ID)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("завдання %s ще виконується — перевірте пізніше", task.ID)
	case err != nil:
		return err
	}
	fmt.Println("внесення зареєстровано:", done.Result.Receipt.FiscalNumber)
	return nil
}
