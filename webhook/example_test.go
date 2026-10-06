package webhook_test

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/prro-cloud/prro-go"
	"github.com/prro-cloud/prro-go/webhook"
)

func ExampleHandler() {
	secret := os.Getenv("PRRO_WEBHOOK_SECRET")

	http.Handle("/prro-hook", webhook.Handler(secret, func(ctx context.Context, e *webhook.Event) error {
		switch e.Type {
		case prro.EventTaskCompleted:
			d, err := e.Task()
			if err != nil {
				return err
			}
			if r := d.Result.Receipt; r != nil {
				log.Printf("чек %s: %s", r.FiscalNumber, r.ReceiptURL)
			}
		case prro.EventClientBalanceLow:
			d, err := e.Balance()
			if err != nil {
				return err
			}
			log.Printf("баланс добігає кінця: лишилося %d чеків", d.ReceiptsLeft)
		}
		return nil
	}))
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func ExampleParseRequest() {
	secret := os.Getenv("PRRO_WEBHOOK_SECRET")

	http.HandleFunc("POST /prro-hook", func(w http.ResponseWriter, r *http.Request) {
		e, err := webhook.ParseRequest(r, secret)
		if errors.Is(err, webhook.ErrInvalidSignature) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// Доставка — «щонайменше один раз»: відкидайте повтори за DeliveryID.
		log.Printf("подія %s, доставка %s, спроба %d", e.Type, e.DeliveryID, e.Attempt)
		w.WriteHeader(http.StatusOK)
	})
}
