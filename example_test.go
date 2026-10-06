package prro_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/prro-cloud/prro-go"
)

func ExampleNewClient() {
	c := prro.NewClient(os.Getenv("PRRO_TOKEN"),
		prro.WithUserAgent("my-shop/1.0"),
	)

	me, err := c.System.WhoAmI(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("клієнт:", me.ClientID)
}

func ExampleReceiptsService_Create() {
	c := prro.NewClient(os.Getenv("PRRO_TOKEN"))
	ctx := context.Background()

	task, err := c.Receipts.Create(ctx, &prro.SettlementReceipt{
		CashRegisterID: os.Getenv("PRRO_REGISTER_ID"),
		Type:           prro.ReceiptSale,
		Items: []prro.ReceiptItem{
			{Name: "Кава американо", Quantity: "1", Price: "65.00", TaxLetters: "А"},
		},
		Payments: []prro.Payment{{Type: prro.PaymentCash, Sum: "65.00", Provided: "100.00"}},
	}, prro.WithIdempotencyKey("order-10412"))
	if err != nil {
		log.Fatal(err)
	}
	if !task.Done() {
		if task, err = c.Tasks.Wait(ctx, task.ID); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Println(task.Result.Receipt.FiscalNumber, task.Result.Receipt.ReceiptURL)
}

func ExampleInvoicesService_All() {
	c := prro.NewClient(os.Getenv("PRRO_TOKEN"))

	for inv, err := range c.Invoices.All(context.Background()) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(inv.PeriodStart, inv.Amount, inv.Status)
	}
}

func ExampleAmountFromKop() {
	fmt.Println(prro.AmountFromKop(25990))
	kop, _ := prro.Amount("65.5").Kop()
	fmt.Println(kop)
	// Output:
	// 259.90
	// 6550
}

func ExampleError() {
	c := prro.NewClient(os.Getenv("PRRO_TOKEN"))

	_, err := c.System.WhoAmI(context.Background())
	if err == nil {
		fmt.Println("токен дійсний")
		return
	}
	if errors.Is(err, prro.ErrUnauthorized) {
		fmt.Println("токен недійсний або відкликаний")
		return
	}
	if apiErr, ok := errors.AsType[*prro.Error](err); ok {
		fmt.Println("помилка API:", apiErr.Code, apiErr.Message)
		return
	}
	fmt.Println("мережева помилка:", err)
}
