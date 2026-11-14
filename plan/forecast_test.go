package plan

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var base = time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

func day(n int) time.Time { return base.AddDate(0, 0, n) }

func item(date time.Time, amount Cents, recurrence string) Item {
	return Item{Date: date, Amount: amount, Recurrence: recurrence}
}

// expected day: offset from base, running balance, amounts landing that day
type want struct {
	offset  int
	balance Cents
	amounts []Cents
}

func assertDays(t *testing.T, got []Day, wants []want) {
	t.Helper()
	var gotW []want
	for _, d := range got {
		w := want{offset: int(d.Date.Sub(base).Hours() / 24), balance: d.Balance}
		for _, o := range d.Occurrences {
			w.amounts = append(w.amounts, o.Amount)
		}
		gotW = append(gotW, w)
	}
	if !reflect.DeepEqual(gotW, wants) {
		t.Errorf("forecast mismatch\n got: %v\nwant: %v", gotW, wants)
	}
}

func mustForecast(t *testing.T, items []Item, through time.Time) []Day {
	t.Helper()
	days, err := Forecast(items, through)
	if err != nil {
		t.Fatal(err)
	}
	return days
}

func TestForecastStopsAtThroughDateInclusive(t *testing.T) {
	items := []Item{
		item(day(0), 0, "never"),
		item(day(1), 1000, "weekly"),
		item(day(2), -1100, "weekly"),
	}

	assertDays(t, mustForecast(t, items, day(3)), []want{
		{0, 0, []Cents{0}},
		{1, 1000, []Cents{1000}},
		{2, -100, []Cents{-1100}},
	})

	assertDays(t, mustForecast(t, items, day(9)), []want{
		{0, 0, []Cents{0}},
		{1, 1000, []Cents{1000}},
		{2, -100, []Cents{-1100}},
		{8, 900, []Cents{1000}},
		{9, -200, []Cents{-1100}},
	})
}

func TestForecastStartsFromBalanceItem(t *testing.T) {
	items := []Item{
		item(day(0), -450, "never"),
		item(day(1), 1000, "weekly"),
		item(day(2), -1100, "weekly"),
	}

	assertDays(t, mustForecast(t, items, day(3)), []want{
		{0, -450, []Cents{-450}},
		{1, 550, []Cents{1000}},
		{2, -550, []Cents{-1100}},
	})
}

func TestForecastCombinesSameDayInItemOrder(t *testing.T) {
	items := []Item{
		item(day(0), 0, "never"),
		item(day(1), 2, "every 3 days"),
		item(day(1), 3, "every 2 days"),
		item(day(1), 4, "every 1 days"),
		item(day(2), -20, "weekly"),
	}

	assertDays(t, mustForecast(t, items, day(14)), []want{
		{0, 0, []Cents{0}},
		{1, 9, []Cents{2, 3, 4}},
		{2, -7, []Cents{4, -20}},
		{3, 0, []Cents{3, 4}},
		{4, 6, []Cents{2, 4}},
		{5, 13, []Cents{3, 4}},
		{6, 17, []Cents{4}},
		{7, 26, []Cents{2, 3, 4}},
		{8, 30, []Cents{4}},
		{9, 17, []Cents{3, 4, -20}},
		{10, 23, []Cents{2, 4}},
		{11, 30, []Cents{3, 4}},
		{12, 34, []Cents{4}},
		{13, 43, []Cents{2, 3, 4}},
		{14, 47, []Cents{4}},
	})
}

func TestForecastCombinesOnFirstDay(t *testing.T) {
	items := []Item{item(day(0), -3, "never"), item(day(0), 2, "every 1 days")}

	assertDays(t, mustForecast(t, items, day(0)), []want{
		{0, -1, []Cents{-3, 2}},
	})
}

func TestForecastIncludesPastDueItems(t *testing.T) {
	items := []Item{
		item(day(0), 10000, "never"),
		item(day(-3), -2500, "once"),
	}

	assertDays(t, mustForecast(t, items, day(5)), []want{
		{-3, -2500, []Cents{-2500}},
		{0, 7500, []Cents{10000}},
	})
}

func TestForecastHonorsItemEndDate(t *testing.T) {
	gym := item(day(0), -500, "weekly")
	gym.End = day(10)

	assertDays(t, mustForecast(t, []Item{gym}, day(30)), []want{
		{0, -500, []Cents{-500}},
		{7, -1000, []Cents{-500}},
	})
}

func TestForecastStepsMonthlyFromOrigin(t *testing.T) {
	rent := Item{
		Date:       time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
		Origin:     time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
		Amount:     -100000,
		Recurrence: "monthly",
	}

	days := mustForecast(t, []Item{rent}, time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC))

	var got []string
	for _, d := range days {
		got = append(got, d.Date.Format("2006-01-02"))
	}
	if want := []string{"2026-02-28", "2026-03-31", "2026-04-30"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestForecastOccurrenceKeepsItsItem(t *testing.T) {
	rent := Item{ID: "rent", Description: "Rent", Date: day(0), Amount: -100, Recurrence: "monthly"}

	days := mustForecast(t, []Item{rent}, day(0))

	o := days[0].Occurrences[0]
	if o.Item.ID != "rent" || o.Item.Description != "Rent" || !o.Date.Equal(day(0)) {
		t.Errorf("got %+v", o)
	}
}

func TestForecastRejectsUnparseableRecurrence(t *testing.T) {
	items := []Item{{ID: "bad", Description: "Gym", Date: day(0), Amount: -5, Recurrence: "biweekly"}}

	_, err := Forecast(items, day(30))

	if err == nil || !strings.Contains(err.Error(), "Gym") || !strings.Contains(err.Error(), "biweekly") {
		t.Errorf("want error naming the item and recurrence, got %v", err)
	}
}

func TestForecastEmpty(t *testing.T) {
	if days := mustForecast(t, nil, day(30)); len(days) != 0 {
		t.Errorf("got %v", days)
	}
}

func TestLowPicksLowestBalanceEarliestOnTie(t *testing.T) {
	days := mustForecast(t, []Item{
		item(day(0), 1000, "never"),
		item(day(1), -1500, "once"),
		item(day(2), 800, "once"),
		item(day(3), -800, "once"),
		item(day(4), 2000, "once"),
	}, day(10))

	low, ok := Low(days)

	if !ok || !low.Date.Equal(day(1)) || low.Balance != -500 {
		t.Errorf("got %v %v, want day 1 at -500", low, ok)
	}
}

func TestLowOfNothing(t *testing.T) {
	if _, ok := Low(nil); ok {
		t.Error("want no low point for an empty forecast")
	}
}
