// Димова перевірка: викликає всі маршрути читання API і нічого не створює.
// Зручно, щоб перевірити токен і доступ або бібліотеку проти локального
// сервера.
//
//	PRRO_TOKEN=… go run ./examples/smoke
//	PRRO_TOKEN=… PRRO_BASE_URL=http://localhost:8083 go run ./examples/smoke
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/prro-cloud/prro-go"
)

func main() { os.Exit(run()) }

// run повертає код завершення: 0 — усі перевірки пройдено.
func run() int {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	opts := []prro.Option{prro.WithBaseURL(os.Getenv("PRRO_BASE_URL"))}
	if os.Getenv("PRRO_DEBUG") != "" {
		h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
		opts = append(opts, prro.WithLogger(slog.New(h)))
	}
	c := prro.NewClient(os.Getenv("PRRO_TOKEN"), opts...)

	failed := 0
	check := func(name string, err error, detail string) {
		switch {
		case err == nil:
			fmt.Printf("✓ %-22s %s\n", name, detail)
		case errors.Is(err, prro.ErrForbidden):
			fmt.Printf("– %-22s пропущено: токен обмежено касами\n", name)
		default:
			failed++
			fmt.Printf("✗ %-22s %v\n", name, err)
		}
	}

	v, err := c.System.Version(ctx)
	check("System.Version", err, v)

	me, err := c.System.WhoAmI(ctx)
	if err != nil {
		check("System.WhoAmI", err, "")
		fmt.Println("без дійсного токена далі перевіряти нічого")
		return 1
	}
	check("System.WhoAmI", nil, fmt.Sprintf("клієнт %s, усі каси: %v", me.ClientID, me.AllCashRegisters))

	dps, err := c.System.DPS(ctx)
	if err == nil {
		check("System.DPS", nil, fmt.Sprintf("%s (вердикт %s)", dps.Effective, dps.Verdict))
	} else {
		check("System.DPS", err, "")
	}

	regs, err := c.CashRegisters.List(ctx)
	check("CashRegisters.List", err, fmt.Sprintf("%d кас", len(regs)))
	for _, r := range regs {
		st, err := c.CashRegisters.Get(ctx, r.ID)
		detail := ""
		if err == nil {
			detail = fmt.Sprintf("%s, зміна %s, режим змін %s", st.Mode, st.State, st.ShiftMode)
			if st.CashBalance != nil {
				detail += ", готівка " + string(st.CashBalance.Amount)
			}
		}
		check("CashRegisters.Get", err, r.FiscalNumber+": "+detail)
	}

	usage, err := c.Billing.Usage(ctx, nil)
	if err == nil {
		check("Billing.Usage", nil, fmt.Sprintf("баланс %s коп., чеків за період %d", usage.Balance, usage.Receipts))
	} else {
		check("Billing.Usage", err, "")
	}

	invoices, err := c.Invoices.List(ctx, nil)
	check("Invoices.List", err, fmt.Sprintf("%d рахунків", len(invoices)))

	hooks, err := c.Webhooks.List(ctx)
	if err == nil {
		check("Webhooks.List", nil, fmt.Sprintf("%d із %d вебхуків", hooks.Used, hooks.Limit))
	} else {
		check("Webhooks.List", err, "")
	}

	if failed > 0 {
		fmt.Printf("\nпомилок: %d\n", failed)
		return 1
	}
	return 0
}
