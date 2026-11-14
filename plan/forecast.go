// Package plan forecasts an account's daily balance from its planned items.
package plan

import (
	"fmt"
	"slices"
	"time"

	"github.com/chrismo/plimsoll/recur"
)

type Cents int64

type Item struct {
	ID          string
	Description string
	Amount      Cents
	Date        time.Time
	Origin      time.Time
	End         time.Time
	Recurrence  string
}

type Occurrence struct {
	Item   *Item
	Date   time.Time
	Amount Cents
}

type Day struct {
	Date        time.Time
	Balance     Cents
	Occurrences []Occurrence
}

// Forecast returns the running balance on each day an item lands, through
// inclusive. Nothing filters on today: an unpaid past item counts.
func Forecast(items []Item, through time.Time) ([]Day, error) {
	var occs []Occurrence
	for i := range items {
		it := &items[i]
		end := through
		if !it.End.IsZero() && it.End.Before(end) {
			end = it.End
		}
		r, err := recur.New(it.Date, it.Recurrence, recur.Options{Origin: it.Origin, End: end})
		if err != nil {
			return nil, fmt.Errorf("item %q: %w", it.Description, err)
		}
		for ; r.Active(); r.Next() {
			occs = append(occs, Occurrence{Item: it, Date: r.Current(), Amount: it.Amount})
		}
	}
	slices.SortStableFunc(occs, func(a, b Occurrence) int { return a.Date.Compare(b.Date) })

	var days []Day
	var balance Cents
	for _, o := range occs {
		balance += o.Amount
		if n := len(days); n > 0 && days[n-1].Date.Equal(o.Date) {
			days[n-1].Balance = balance
			days[n-1].Occurrences = append(days[n-1].Occurrences, o)
		} else {
			days = append(days, Day{Date: o.Date, Balance: balance, Occurrences: []Occurrence{o}})
		}
	}
	return days, nil
}

// Low returns the day with the lowest balance, the earliest one on a tie.
func Low(days []Day) (Day, bool) {
	if len(days) == 0 {
		return Day{}, false
	}
	low := days[0]
	for _, d := range days[1:] {
		if d.Balance < low.Balance {
			low = d
		}
	}
	return low, true
}
