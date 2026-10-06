// Приймач вебхуків: перевіряє підпис, відкидає повтори доставок і реагує на
// події чеків, каси і балансу.
//
//	PRRO_WEBHOOK_SECRET=… go run ./examples/webhook
//
// Адреса приймача для вебхука — http://<хост>:8080/prro-hook.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/prro-cloud/prro-go"
	"github.com/prro-cloud/prro-go/webhook"
)

// seen — оброблені доставки. Доставка гарантована «щонайменше один раз»,
// тож повтори відкидаються за DeliveryID. У продакшені — таблиця з
// унікальним ключем у тій самій транзакції, що й обробка.
var seen sync.Map

func handle(ctx context.Context, e *webhook.Event) error {
	log.Printf("подія %s: доставка %s, спроба %d", e.Type, e.DeliveryID, e.Attempt)
	if _, dup := seen.LoadOrStore(e.DeliveryID, struct{}{}); dup {
		return nil
	}

	switch e.Type {
	case prro.EventTaskCompleted:
		d, err := e.Task()
		if err != nil {
			return err
		}
		if r := d.Result.Receipt; r != nil {
			log.Printf("чек %s видано: %s", r.FiscalNumber, r.ReceiptURL)
		}

	case prro.EventTaskFailed:
		d, err := e.Task()
		if err != nil {
			return err
		}
		log.Printf("завдання %s не виконано: %s", d.TaskID, d.Error)

	case prro.EventRegisterOfflineLimit:
		d, err := e.Offline()
		if err != nil {
			return err
		}
		log.Printf("каса %s офлайн уже %s — закрийте сесію", d.CashRegisterID, d.SessionUsed)

	case prro.EventClientBalanceLow:
		d, err := e.Balance()
		if err != nil {
			return err
		}
		log.Printf("баланс добігає кінця: лишилося %d чеків", d.ReceiptsLeft)
	}
	return nil
}

func main() {
	mux := http.NewServeMux()
	mux.Handle("POST /prro-hook", webhook.Handler(os.Getenv("PRRO_WEBHOOK_SECRET"), handle))

	srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Println("слухаю :8080/prro-hook")
	log.Fatal(srv.ListenAndServe())
}
