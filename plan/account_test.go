package plan

import (
	"errors"
	"reflect"
	"strings"
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

func TestPayWithoutExpectedDateRefusesItemNotYetDue(t *testing.T) {
	a := newAccount(0, today, named("p", today, -5, "weekly"), named("rent", day(1), -1000, "monthly"))

	err := a.Pay(today, Ref{ID: "p"}, Ref{ID: "rent"})

	var notDue *NotDue
	if !errors.As(err, &notDue) || notDue.ID != "rent" || !notDue.Next.Equal(day(1)) {
		t.Fatalf("want NotDue for rent on %s, got %v", day(1).Format(time.DateOnly), err)
	}
	if want := newAccount(0, today, named("p", today, -5, "weekly"), named("rent", day(1), -1000, "monthly")); !reflect.DeepEqual(a, want) {
		t.Errorf("account changed:\n got %+v\nwant %+v", *a, *want)
	}
}

func TestPayWithoutExpectedDateTwiceRefusesTheSecond(t *testing.T) {
	a := newAccount(0, today, named("p", today, -5, "weekly"))

	if err := a.Pay(today, Ref{ID: "p"}); err != nil {
		t.Fatal(err)
	}
	err := a.Pay(today, Ref{ID: "p"})

	if !errors.As(err, new(*NotDue)) {
		t.Fatalf("want NotDue, got %v", err)
	}
	if a.Balance != -5 {
		t.Errorf("balance: got %d, want -5", a.Balance)
	}
}

func TestSkipWithoutExpectedDateRefusesItemNotYetDue(t *testing.T) {
	a := newAccount(0, today, named("rent", day(1), -1000, "monthly"))

	err := a.Skip(today, Ref{ID: "rent"})

	if !errors.As(err, new(*NotDue)) {
		t.Fatalf("want NotDue, got %v", err)
	}
	assertDate(t, "rent", find(t, a, "rent").Date, day(1))
}

func TestPayWithExpectedDateCanPayEarly(t *testing.T) {
	a := newAccount(0, today, named("rent", day(1), -1000, "monthly"))

	if err := a.Pay(today, Ref{ID: "rent", Expected: day(1)}); err != nil {
		t.Fatal(err)
	}
	if a.Balance != -1000 {
		t.Errorf("balance: got %d, want -1000", a.Balance)
	}
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
	_ = a.Pay(mon, Ref{ID: "t", Expected: time.Date(2013, 9, 6, 0, 0, 0, 0, time.UTC)})
	assertDate(t, "second pay", find(t, a, "t").Date, time.Date(2013, 9, 9, 0, 0, 0, 0, time.UTC))
	assertDate(t, "origin", find(t, a, "t").Origin, mon)
}

func TestPayHealsShortMonthDrift(t *testing.T) {
	jan31 := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	a := newAccount(0, jan31, named("rent", jan31, -1, "monthly"))

	_ = a.Pay(jan31, Ref{ID: "rent"})
	assertDate(t, "feb", find(t, a, "rent").Date, time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC))
	_ = a.Pay(jan31, Ref{ID: "rent", Expected: time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)})
	assertDate(t, "mar", find(t, a, "rent").Date, time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC))
}

func TestPayStopsAtEndDate(t *testing.T) {
	w := named("w", today, -1, "weekly")
	w.End = day(21)
	a := newAccount(0, today, w)

	next := today
	for _, want := range []time.Time{day(7), day(14), day(21)} {
		if err := a.Pay(today, Ref{ID: "w", Expected: next}); err != nil {
			t.Fatal(err)
		}
		assertDate(t, "pay", find(t, a, "w").Date, want)
		next = want
	}
	_ = a.Pay(today, Ref{ID: "w", Expected: next})
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

func ptr[T any](v T) *T { return &v }

func editable() *Account {
	return newAccount(500, day(-1),
		Item{ID: "rent", Description: "Rent", Date: day(1), Origin: day(-30), Amount: -100000, Recurrence: "monthly"},
		named("gym", day(3), -2500, "weekly"),
	)
}

func TestEditChangesOnlyGivenFields(t *testing.T) {
	a := editable()

	got, err := a.Edit(Ref{ID: "rent", Expected: day(1)}, ptr(Cents(-100000)), Change{Amount: ptr(Cents(-110000))})
	if err != nil {
		t.Fatal(err)
	}
	want := Item{ID: "rent", Description: "Rent", Date: day(1), Origin: day(-30), Amount: -110000, Recurrence: "monthly"}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(find(t, a, "rent"), want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if a.Balance != 500 {
		t.Errorf("edit changed the balance to %d", a.Balance)
	}
}

func TestEditMovingDateResetsOrigin(t *testing.T) {
	a := editable()

	got, err := a.Edit(Ref{ID: "rent", Expected: day(1)}, nil, Change{Date: ptr(day(5))})
	if err != nil {
		t.Fatal(err)
	}
	assertDate(t, "date", got.Date, day(5))
	assertDate(t, "origin", got.Origin, day(5))
}

func TestEditOtherFieldsKeepOrigin(t *testing.T) {
	a := editable()

	got, err := a.Edit(Ref{ID: "rent", Expected: day(1)}, nil,
		Change{Description: ptr("Mortgage"), Recurrence: ptr("every 2 weeks"), End: ptr(day(90))})
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Mortgage" || got.Recurrence != "every 2 weeks" {
		t.Errorf("got %+v", got)
	}
	assertDate(t, "end", got.End, day(90))
	assertDate(t, "origin", got.Origin, day(-30))
}

func TestEditZeroEndClearsIt(t *testing.T) {
	a := editable()
	a.Items[0].End = day(90)

	got, err := a.Edit(Ref{ID: "rent", Expected: day(1)}, nil, Change{End: ptr(time.Time{})})

	if err != nil || !got.End.IsZero() {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestEditRefusesAndChangesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		ref            Ref
		expectedAmount *Cents
		change         Change
		check          func(error) bool
	}{
		"no expected date": {
			Ref{ID: "rent"},
			nil,
			Change{Amount: ptr(Cents(1))},
			func(err error) bool { return errors.Is(err, ErrExpectedDate) },
		},
		"date mismatch": {
			Ref{ID: "rent", Expected: day(2)},
			nil,
			Change{Amount: ptr(Cents(1))},
			func(err error) bool { var m *OccurrenceMismatch; return errors.As(err, &m) },
		},
		"amount mismatch": {
			Ref{ID: "rent", Expected: day(1)},
			ptr(Cents(-99999)),
			Change{Amount: ptr(Cents(1))},
			func(err error) bool {
				var m *AmountMismatch
				return errors.As(err, &m) && m.ID == "rent" && m.Expected == -99999 && m.Current == -100000
			},
		},
		"unknown id": {
			Ref{ID: "nope", Expected: day(1)},
			nil,
			Change{Amount: ptr(Cents(1))},
			func(err error) bool { return errors.Is(err, ErrNotFound) },
		},
		"nothing to change": {
			Ref{ID: "rent", Expected: day(1)},
			nil,
			Change{},
			func(err error) bool { return errors.Is(err, ErrNoChange) },
		},
		"bad recurrence": {
			Ref{ID: "rent", Expected: day(1)},
			nil,
			Change{Recurrence: ptr("biweekly")},
			func(err error) bool { return err != nil && strings.Contains(err.Error(), "biweekly") },
		},
		"end before date": {
			Ref{ID: "rent", Expected: day(1)},
			nil,
			Change{End: ptr(day(0))},
			func(err error) bool { return err != nil && strings.Contains(err.Error(), "end") },
		},
		"date moved past end": {
			Ref{ID: "gym", Expected: day(3)},
			nil,
			Change{Date: ptr(day(100)), End: ptr(day(50))},
			func(err error) bool { return err != nil && strings.Contains(err.Error(), "end") },
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := editable()
			before := editable()

			_, err := a.Edit(tc.ref, tc.expectedAmount, tc.change)

			if !tc.check(err) {
				t.Errorf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(a, before) {
				t.Errorf("account changed:\n got %+v\nwant %+v", a, before)
			}
		})
	}
}

func TestAddAppends(t *testing.T) {
	a := editable()
	gift := named("gift", day(9), -5000, "once")

	if err := a.Add(gift, false); err != nil {
		t.Fatal(err)
	}
	if got := a.Items[len(a.Items)-1]; !reflect.DeepEqual(got, gift) {
		t.Errorf("got %+v", got)
	}
}

// Same date, amount, description and recurrence as an existing item means
// a retried add, as in Cashew. The end date doesn't count.
func TestAddRefusesDuplicate(t *testing.T) {
	a := editable()
	again := Item{ID: "rent-2", Description: "Rent", Date: day(1), Amount: -100000, Recurrence: "monthly", End: day(99)}

	err := a.Add(again, false)

	var dup *Duplicate
	if !errors.As(err, &dup) || dup.ID != "rent" {
		t.Fatalf("want Duplicate of rent, got %v", err)
	}
	if len(a.Items) != 2 {
		t.Errorf("duplicate was added: %+v", a.Items)
	}

	if err := a.Add(again, true); err != nil || len(a.Items) != 3 {
		t.Errorf("allowDuplicate should add it: %v, %d items", err, len(a.Items))
	}
}

func TestAddAnyDifferenceIsNotDuplicate(t *testing.T) {
	base := Item{ID: "x", Description: "Rent", Date: day(1), Amount: -100000, Recurrence: "monthly"}
	for name, change := range map[string]func(*Item){
		"date":        func(it *Item) { it.Date = day(2) },
		"amount":      func(it *Item) { it.Amount = -1 },
		"description": func(it *Item) { it.Description = "Rent (garage)" },
		"recurrence":  func(it *Item) { it.Recurrence = "once" },
	} {
		it := base
		change(&it)
		if err := editable().Add(it, false); err != nil {
			t.Errorf("%s differs, want added, got %v", name, err)
		}
	}
}
