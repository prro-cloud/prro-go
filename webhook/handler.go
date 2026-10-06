package webhook

import (
	"context"
	"errors"
	"net/http"
)

// HandlerFunc обробляє одну перевірену доставку. Помилка означає «не
// оброблено»: приймач відповідає 500, і сервіс повторить доставку пізніше.
type HandlerFunc func(ctx context.Context, e *Event) error

// Handler повертає [http.Handler], що приймає доставки вебхуків:
// перевіряє підпис, розбирає конверт і викликає fn. secret — секрет
// вебхука, виданий під час його створення (див. розділ «Секрет» в описі
// пакета).
//
// Відповіді: 200 — подію оброблено; 401 — підпис недійсний; 405 — не POST;
// 413 — тіло понад [MaxBodyBytes]; 400 — конверт не розібрано; 500 — fn
// повернула помилку. Сервіс вважає успіхом будь-яку 2xx-відповідь і
// повторює решту, тож fn має бути ідемпотентною щодо [Event.DeliveryID].
func Handler(secret string, fn HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		e, err := ParseRequest(r, secret)
		switch {
		case errors.Is(err, ErrInvalidSignature):
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		case errors.Is(err, errBodyTooLarge):
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		case err != nil:
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := fn(r.Context(), e); err != nil {
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
