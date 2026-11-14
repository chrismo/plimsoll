package plan

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

var today = day(0)

func newAccount(balance Cents, asOf time.Time, items ...Item) *Account {
	return &Account{Balance: balance, AsOf: asOf, Items: items}
}

func named(id string, date time.Time, amount Cents, recurrence string) Item {
	return Item{ID: id, Date: date, Amount: amount, Recurrence: recurrence}
}

func find(t *testing.T, a *Account, id string) Item {
	t.Helper()
	for _, it := range a.Items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("item %q not found", id)
	return Item{}
}

func assertDate(t *testing.T, label string, got, want time.Time) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s: got %s, want %s", label, got.Format("2006-01-02"), want.Format("2006-01-02"))
	}
}

func TestPayAdjustsBalanceAndAdvancesEachItem(t *testing.T) {
	a := newAccount(10000, day(-3),
		named("rent", day(-2), -100000, "monthly"),
		named("pay", day(-1), 250000, "every 2 weeks"),
	)

	err := a.Pay(today, Ref{ID: "rent", Expected: day(-2)}, Ref{ID: "pay", Expected: day(-1)})

	if err != nil {
		t.Fatal(err)
	}
	if a.Balance != 160000 {
		t.Errorf("balance: got %d, want 160000", a.Balance)
	}
	assertDate(t, "rent", find(t, a, "rent").Date, day(-2).AddDate(0, 1, 0))
	assertDate(t, "pay", find(t, a, "pay").Date, day(13))
	assertDate(t, "as of", a.AsOf, today)
}

func TestPayConsecutiveOccurrencesOfOneItem(t *testing.T) {
	a := newAccount(0, day(-8), named("w", day(-8), -1000, "weekly"))

	err := a.Pay(today, Ref{ID: "w", Expected: day(-8)}, Ref{ID: "w", Expected: day(-1)})

	if err != nil {
		t.Fatal(err)
	}
	assertDate(t, "weekly", find(t, a, "w").Date, day(6))
	if a.Balance != -2000 {
		t.Errorf("balance: got %d, want -2000", a.Balance)
	}
}

func TestPayChangesNothingWhenAnyExpectedDateMismatches(t *testing.T) {
	a := newAccount(0, day(-3),
		named("rent", day(-2), -100000, "monthly"),
		named("pay", day(-1), 250000, "every 2 weeks"),
	)
	before := *a
	before.Items = append([]Item(nil), a.Items...)

	err := a.Pay(today, Ref{ID: "rent", Expected: day(-2)}, Ref{ID: "pay", Expected: today})

	var mismatch *OccurrenceMismatch
	if !errors.As(err, &mismatch) {
		t.Fatalf("want OccurrenceMismatch, got %v", err)
	}
	if mismatch.ID != "pay" || !mismatch.Expected.Equal(today) || !mismatch.Next.Equal(day(-1)) {
		t.Errorf("got %+v", mismatch)
	}
	if !reflect.DeepEqual(*a, before) {
		t.Errorf("account changed:\n got %+v\nwant %+v", *a, before)
	}
}

func TestPayWithoutExpectedDateSkipsTheCheck(t *testing.T) {
	a := newAccount(0, day(-1), named("p", day(-1), -1000, "weekly"))

	if err := a.Pay(today, Ref{ID: "p"}); err != nil {
		t.Fatal(err)
	}
	if a.Balance != -1000 {
		t.Errorf("balance: got %d, want -1000", a.Balance)
	}
	assertDate(t, "p", find(t, a, "p").Date, day(6))
}

func TestPayUnknownItem(t *testing.T) {
	a := newAccount(0, today, named("p", today, -1000, "weekly"))

	err := a.Pay(today, Ref{ID: "nope"})

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestPayOnceItemRemovesIt(t *testing.T) {
	a := newAccount(0, today, named("o", today, -1000, "once"), named("w", day(2), -5, "weekly"))

	if err := a.Pay(today, Ref{ID: "o"}); err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != 1 || a.Items[0].ID != "w" {
		t.Errorf("got %+v", a.Items)
	}
}

// Cashew refused to pay "never" because only its Current Balance row used
// it. With a real balance there's no reason to: "never" means "once".
func TestPayNeverItemRemovesIt(t *testing.T) {
	a := newAccount(0, today, named("n", today, -1000, "never"))

	if err := a.Pay(today, Ref{ID: "n"}); err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != 0 || a.Balance != -1000 {
		t.Errorf("got %+v", a)
	}
}

func TestPayKeepsOriginSoUnevenSpacingHoldsItsRhythm(t *testing.T) {
	mon := time.Date(2013, 9, 2, 0, 0, 0, 0, time.UTC)
	a := newAccount(0, mon, named("t", mon, -1, "2 times a week"))

	_ = a.Pay(mon, Ref{ID: "t"})
	assertDate(t, "first pay", find(t, a, "t").Date, time.Date(2013, 9, 6, 0, 0, 0, 0, time.UTC))
	_ = a.Pay(mon, Ref{ID: "t"})
	assertDate(t, "second pay", find(t, a, "t").Date, time.Date(2013, 9, 9, 0, 0, 0, 0, time.UTC))
	assertDate(t, "origin", find(t, a, "t").Origin, mon)
}

func TestPayHealsShortMonthDrift(t *testing.T) {
	jan31 := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	a := newAccount(0, jan31, named("rent", jan31, -1, "monthly"))

	_ = a.Pay(jan31, Ref{ID: "rent"})
	assertDate(t, "feb", find(t, a, "rent").Date, time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC))
	_ = a.Pay(jan31, Ref{ID: "rent"})
	assertDate(t, "mar", find(t, a, "rent").Date, time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC))
}

func TestPayStopsAtEndDate(t *testing.T) {
	w := named("w", today, -1, "weekly")
	w.End = day(21)
	a := newAccount(0, today, w)

	for _, want := range []time.Time{day(7), day(14), day(21)} {
		if err := a.Pay(today, Ref{ID: "w"}); err != nil {
			t.Fatal(err)
		}
		assertDate(t, "pay", find(t, a, "w").Date, want)
	}
	_ = a.Pay(today, Ref{ID: "w"})
	if len(a.Items) != 0 {
		t.Errorf("want item removed after its end date, got %+v", a.Items)
	}
}

func TestSkipAdvancesWithoutTouchingBalance(t *testing.T) {
	a := newAccount(500, day(-1), named("p", day(-1), -1000, "weekly"))

	if err := a.Skip(today, Ref{ID: "p", Expected: day(-1)}); err != nil {
		t.Fatal(err)
	}
	assertDate(t, "p", find(t, a, "p").Date, day(6))
	if a.Balance != 500 {
		t.Errorf("balance: got %d, want 500", a.Balance)
	}
	assertDate(t, "as of", a.AsOf, today)
}

func TestSkipMismatchChangesNothing(t *testing.T) {
	a := newAccount(0, day(-1), named("p", day(-1), -1000, "weekly"))

	err := a.Skip(today, Ref{ID: "p", Expected: day(-8)})

	var mismatch *OccurrenceMismatch
	if !errors.As(err, &mismatch) {
		t.Fatalf("want OccurrenceMismatch, got %v", err)
	}
	assertDate(t, "p", find(t, a, "p").Date, day(-1))
}

func TestSkipLastItemLeavesAsOf(t *testing.T) {
	a := newAccount(0, day(-1), named("o", day(-1), -1000, "once"))

	if err := a.Skip(today, Ref{ID: "o"}); err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != 0 {
		t.Errorf("got %+v", a.Items)
	}
	assertDate(t, "as of", a.AsOf, day(-1))
}

// After a pay or skip, the balance date moves up to the earliest remaining
// item, but never past today.
func TestAsOfMovesToEarliestItemCappedAtToday(t *testing.T) {
	a := newAccount(0, day(-10), named("a", day(-5), -1, "once"), named("b", day(-3), -1, "weekly"))

	_ = a.Pay(today, Ref{ID: "a"})
	assertDate(t, "after paying a", a.AsOf, day(-3))

	_ = a.Pay(today, Ref{ID: "b"})
	assertDate(t, "after paying b", a.AsOf, today)
}

// Cashew only moves the balance date when the balance sorts before every
// item. An item dated before the balance date leaves it alone.
func TestAsOfStaysWhenAnItemPredatesIt(t *testing.T) {
	a := newAccount(0, day(-1), named("old", day(-5), -1, "weekly"), named("p", day(-1), -1, "weekly"))

	_ = a.Pay(today, Ref{ID: "p"})

	assertDate(t, "as of", a.AsOf, day(-1))
}
