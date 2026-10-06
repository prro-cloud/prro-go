package prro

import (
	"math"
	"testing"
	"time"
)

func TestAmountFromKop(t *testing.T) {
	tests := map[int64]Amount{
		0:             "0.00",
		5:             "0.05",
		6500:          "65.00",
		25990:         "259.90",
		-1230:         "-12.30",
		math.MinInt64: "-92233720368547758.08",
	}
	for kop, want := range tests {
		if got := AmountFromKop(kop); got != want {
			t.Errorf("AmountFromKop(%d) = %q, want %q", kop, got, want)
		}
	}
}

func TestAmountKop(t *testing.T) {
	valid := map[Amount]int64{
		"65.00": 6500, "65.5": 6550, "65": 6500, "0.05": 5, "-12.30": -1230, "007.10": 710,
	}
	for a, want := range valid {
		if got, err := a.Kop(); err != nil || got != want {
			t.Errorf("%q.Kop() = %d, %v; want %d", a, got, err, want)
		}
	}
	for _, a := range []Amount{"", "-", ".5", "1.", "1.234", "1,50", "+1", "1e3", " 1", "--1", "99999999999999999999"} {
		if got, err := a.Kop(); err == nil {
			t.Errorf("%q.Kop() = %d, want error", a, got)
		}
	}
}

func FuzzAmountRoundTrip(f *testing.F) {
	for _, kop := range []int64{0, 1, -1, 6500, math.MaxInt64 / 100, math.MinInt64 / 100} {
		f.Add(kop)
	}
	f.Fuzz(func(t *testing.T, kop int64) {
		if kop > math.MaxInt64-99 || kop < -(math.MaxInt64-99) {
			t.Skip()
		}
		got, err := AmountFromKop(kop).Kop()
		if err != nil || got != kop {
			t.Fatalf("round trip %d → %q → %d, %v", kop, AmountFromKop(kop), got, err)
		}
	})
}

func TestDate(t *testing.T) {
	kyiv := time.FixedZone("EEST", 3*3600)
	d := DateOf(time.Date(2026, 7, 14, 23, 30, 0, 0, kyiv))
	if d != "2026-07-14" {
		t.Errorf("DateOf = %q", d)
	}
	tm, err := d.Time()
	if err != nil || !tm.Equal(time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("Time() = %v, %v", tm, err)
	}
}
