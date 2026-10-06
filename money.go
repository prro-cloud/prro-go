package prro

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Amount — сума в гривнях десятковим рядком ("259.90"), у тій самій формі,
// що в чеках: до 2 знаків після коми. Гроші в API ніколи не передаються
// числом з рухомою комою.
//
// Літерал рядка присвоюється напряму — Price: "65.00"; для сум, обчислених
// у копійках, є [AmountFromKop].
type Amount string

// AmountFromKop перетворює суму в копійках на [Amount]: 6500 → "65.00".
func AmountFromKop(kop int64) Amount {
	sign, u := "", uint64(kop)
	if kop < 0 {
		sign, u = "-", uint64(-(kop+1))+1
	}
	return Amount(fmt.Sprintf("%s%d.%02d", sign, u/100, u%100))
}

var errInvalidAmount = errors.New("prro: invalid amount")

// Kop повертає суму в копійках: "65.5" → 6550. Рядок, що не є десятковим
// числом із щонайбільше 2 знаками після коми, — помилка.
func (a Amount) Kop() (int64, error) {
	s := string(a)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" || len(frac) > 2 || (hasDot && frac == "") || !digits(whole) || !digits(frac) {
		return 0, fmt.Errorf("%w %q", errInvalidAmount, string(a))
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > (math.MaxInt64-99)/100 {
		return 0, fmt.Errorf("%w %q: out of range", errInvalidAmount, string(a))
	}
	f, _ := strconv.ParseInt((frac + "00")[:2], 10, 64)
	kop := w*100 + f
	if neg {
		kop = -kop
	}
	return kop, nil
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Kopecks — сума тарифікації в копійках десятковим рядком із точністю до
// 4 знаків ("150.0000" — півтори гривні). Так рахує білінг: ціна чека,
// баланс, рахунки. Суми в чеках — [Amount].
type Kopecks string

// Date — календарна дата у форматі РРРР-ММ-ДД ("2026-07-14").
type Date string

// DateOf повертає дату моменту t у його часовому поясі.
func DateOf(t time.Time) Date { return Date(t.Format(time.DateOnly)) }

// Time повертає початок дати d за UTC.
func (d Date) Time() (time.Time, error) { return time.Parse(time.DateOnly, string(d)) }
