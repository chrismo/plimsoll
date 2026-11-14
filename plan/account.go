package plan

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/chrismo/plimsoll/recur"
)

type Account struct {
	Name    string
	Balance Cents
	AsOf    time.Time
	Items   []Item
}

// Ref names the item to pay or skip. A non-zero Expected must match the
// item's next date, so a retried command can't apply twice.
type Ref struct {
	ID       string
	Expected time.Time
}

var ErrNotFound = errors.New("item not found")

type OccurrenceMismatch struct {
	ID       string
	Expected time.Time
	Next     time.Time
}

func (e *OccurrenceMismatch) Error() string {
	return fmt.Sprintf("item %q next occurs %s, not %s", e.ID, e.Next.Format(time.DateOnly), e.Expected.Format(time.DateOnly))
}

func (a *Account) Forecast(through time.Time) ([]Day, error) {
	return forecast(a.Balance, a.AsOf, a.Items, through)
}

// Pay adds each item's amount to the balance and advances the item to its
// next date, removing it when it has none. Either every ref applies or none.
func (a *Account) Pay(today time.Time, refs ...Ref) error {
	return a.advance(today, refs, true)
}

// Skip advances each item like Pay but leaves the balance alone.
func (a *Account) Skip(today time.Time, refs ...Ref) error {
	return a.advance(today, refs, false)
}

func (a *Account) advance(today time.Time, refs []Ref, pay bool) error {
	next := *a
	next.Items = slices.Clone(a.Items)
	for _, ref := range refs {
		i := slices.IndexFunc(next.Items, func(it Item) bool { return it.ID == ref.ID })
		if i < 0 {
			return fmt.Errorf("%w: %q", ErrNotFound, ref.ID)
		}
		it := &next.Items[i]
		if !ref.Expected.IsZero() && !it.Date.Equal(ref.Expected) {
			return &OccurrenceMismatch{ID: it.ID, Expected: ref.Expected, Next: it.Date}
		}
		if pay {
			next.Balance += it.Amount
		}
		if it.Origin.IsZero() {
			it.Origin = it.Date
		}
		r, err := recur.New(it.Date, it.Recurrence, recur.Options{Origin: it.Origin, End: it.End})
		if err != nil {
			return fmt.Errorf("item %q: %w", it.Description, err)
		}
		if d, ok := r.Next(); ok {
			it.Date = d
		} else {
			next.Items = slices.Delete(next.Items, i, i+1)
		}
		next.moveAsOf(today)
	}
	*a = next
	return nil
}

// moveAsOf follows Cashew: when the balance date is on or before every item,
// it moves up to the earliest item, capped at today.
func (a *Account) moveAsOf(today time.Time) {
	if len(a.Items) == 0 {
		return
	}
	earliest := slices.MinFunc(a.Items, func(x, y Item) int { return x.Date.Compare(y.Date) }).Date
	if a.AsOf.After(earliest) {
		return
	}
	a.AsOf = earliest
	if today.Before(earliest) {
		a.AsOf = today
	}
}
