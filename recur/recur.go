// Package recur steps a planned transaction's date through its recurrence
// ("monthly", "every 2 weeks", "2 times a month", "once", ...).
package recur

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	// Origin is the first date of the series. Zero means the current date.
	Origin time.Time
	// End is the last date the recurrence may land on. Zero means no end.
	End time.Time
}

type Recurrence struct {
	current time.Time
	end     time.Time
	done    bool
	inc     incrementer
}

type incrementer interface {
	next() (time.Time, bool)
}

func New(current time.Time, interval string, opts Options) (*Recurrence, error) {
	inc, err := parse(interval, current, opts.Origin)
	if err != nil {
		return nil, err
	}
	return &Recurrence{current: current, end: opts.End, inc: inc}, nil
}

func (r *Recurrence) Current() time.Time { return r.current }

func (r *Recurrence) Active() bool {
	return !r.done && (r.end.IsZero() || !r.current.After(r.end))
}

// Next advances to the following date. It reports false once the series has
// ended, either by its own rule or by passing the end date.
func (r *Recurrence) Next() (time.Time, bool) {
	d, ok := r.inc.next()
	if !ok {
		r.done = true
		return time.Time{}, false
	}
	r.current = d
	if !r.Active() {
		return time.Time{}, false
	}
	return d, true
}

var (
	everyRe = regexp.MustCompile(`(?i)^every (\d+) (\w+)$`)
	timesRe = regexp.MustCompile(`(?i)^(\d+) times a (\w+)$`)
)

func parse(interval string, current, origin time.Time) (incrementer, error) {
	interval = strings.Join(strings.Fields(interval), " ")
	if m := everyRe.FindStringSubmatch(interval); m != nil {
		amount, _ := strconv.Atoi(m[1])
		u, err := parseUnit(m[2])
		if err != nil {
			return nil, fmt.Errorf("cannot parse interval %q: %w", interval, err)
		}
		if amount == 0 {
			return neverInc{}, nil
		}
		return newEveryFrom(u, amount, current, origin), nil
	}
	if m := timesRe.FindStringSubmatch(interval); m != nil {
		times, _ := strconv.Atoi(m[1])
		u, err := parseUnit(m[2])
		if err != nil {
			return nil, fmt.Errorf("cannot parse interval %q: %w", interval, err)
		}
		if times == 0 {
			return neverInc{}, nil
		}
		if origin.IsZero() {
			return newTimes(u, times, current, time.Time{}), nil
		}
		return newTimes(u, times, origin, current), nil
	}
	switch strings.ToLower(interval) {
	case "weekly":
		return newEvery(days, 7, current), nil
	case "monthly":
		return newEveryFrom(months, 1, current, origin), nil
	case "yearly", "annually":
		return newEveryFrom(years, 1, current, origin), nil
	case "never", "once":
		return neverInc{}, nil
	}
	return nil, fmt.Errorf("cannot parse interval %q", interval)
}

type unit int

const (
	days unit = iota
	weeks
	months
	years
)

func parseUnit(s string) (unit, error) {
	switch strings.TrimSuffix(strings.ToLower(s), "s") {
	case "day":
		return days, nil
	case "week":
		return weeks, nil
	case "month":
		return months, nil
	case "year":
		return years, nil
	}
	return 0, fmt.Errorf("unknown unit %q", s)
}

// lengthInDays matches Cashew's Rails durations: a month is 30 days, a year 365.
func (u unit) lengthInDays() int {
	return [...]int{1, 7, 30, 365}[u]
}

func (u unit) add(t time.Time, n int) time.Time {
	switch u {
	case days:
		return t.AddDate(0, 0, n)
	case weeks:
		return t.AddDate(0, 0, 7*n)
	case months:
		return addMonthsClamped(t, n)
	default:
		return addMonthsClamped(t, 12*n)
	}
}

// addMonthsClamped keeps the day of month, falling back to the month's last
// day when it is shorter (Jan 31 + 1 month = Feb 28). time.AddDate would
// overflow into the next month instead.
func addMonthsClamped(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(d, last)-1)
}

func daysBetween(a, b time.Time) int {
	return int(math.Round(b.Sub(a).Hours() / 24))
}

type neverInc struct{}

func (neverInc) next() (time.Time, bool) { return time.Time{}, false }

type every struct {
	unit   unit
	amount int
	start  time.Time
	steps  int
}

func newEvery(u unit, amount int, start time.Time) *every {
	return &every{unit: u, amount: amount, start: start}
}

// newEveryFrom counts month and year steps from the origin so its day of
// month survives short months (31st -> Feb 28 -> Mar 31).
func newEveryFrom(u unit, amount int, current, origin time.Time) *every {
	if u == days || u == weeks || origin.IsZero() || origin.After(current) {
		return newEvery(u, amount, current)
	}
	e := newEvery(u, amount, origin)
	y1, m1, _ := origin.Date()
	y2, m2, _ := current.Date()
	unitMonths := amount
	if u == years {
		unitMonths = 12 * amount
	}
	e.steps = ((y2-y1)*12 + int(m2-m1)) / unitMonths
	for e.current().After(current) {
		e.steps--
	}
	return e
}

func (e *every) current() time.Time { return e.unit.add(e.start, e.amount*e.steps) }

func (e *every) next() (time.Time, bool) {
	e.steps++
	return e.current(), true
}

func (e *every) lengthInDays() int { return e.amount * e.unit.lengthInDays() }

// times spreads N dates evenly through each unit, restarting at every
// fencepost (origin + k units) so the series never drifts off its origin.
type times struct {
	fence     *every
	count     int
	gap       int
	lastFence time.Time
	queue     []time.Time
}

func newTimes(u unit, count int, origin, previous time.Time) *times {
	t := &times{
		fence: newEvery(u, 1, origin),
		count: count,
		gap:   int(math.Ceil(float64(u.lengthInDays()) / float64(count))),
	}
	if previous.IsZero() {
		t.lastFence = origin
	} else {
		t.lastFence = t.fencepostAtOrBefore(previous)
	}
	t.fill()
	if !previous.IsZero() {
		for !previous.Before(t.queue[0]) {
			t.queue = t.queue[1:]
			if len(t.queue) == 0 {
				t.fill()
			}
		}
	}
	return t
}

func (t *times) fencepostAtOrBefore(d time.Time) time.Time {
	for daysBetween(t.fence.current(), d) >= t.fence.lengthInDays() {
		t.fence.next()
	}
	return t.fence.current()
}

func (t *times) fill() {
	nextFence, _ := t.fence.next()
	d := t.lastFence.AddDate(0, 0, t.gap)
	for d.Before(nextFence) && len(t.queue) < t.count-1 {
		t.queue = append(t.queue, d)
		d = d.AddDate(0, 0, t.gap)
	}
	t.queue = append(t.queue, nextFence)
	t.lastFence = nextFence
}

func (t *times) next() (time.Time, bool) {
	if len(t.queue) == 0 {
		t.fill()
	}
	d := t.queue[0]
	t.queue = t.queue[1:]
	return d, true
}
