package recur

import (
	"testing"
	"time"
)

func d(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

var start = d(2025, 6, 15)

func mustNew(t *testing.T, current time.Time, interval string, opts Options) *Recurrence {
	t.Helper()
	r, err := New(current, interval, opts)
	if err != nil {
		t.Fatalf("New(%s, %q): %v", current.Format(time.DateOnly), interval, err)
	}
	return r
}

func expectNext(t *testing.T, r *Recurrence, want ...time.Time) {
	t.Helper()
	for i, w := range want {
		got, ok := r.Next()
		if !ok {
			t.Fatalf("step %d: got no date, want %s", i+1, w.Format(time.DateOnly))
		}
		if !got.Equal(w) {
			t.Fatalf("step %d: got %s, want %s", i+1, got.Format(time.DateOnly), w.Format(time.DateOnly))
		}
	}
}

func expectDone(t *testing.T, r *Recurrence) {
	t.Helper()
	if got, ok := r.Next(); ok {
		t.Fatalf("got %s, want no more dates", got.Format(time.DateOnly))
	}
	if r.Active() {
		t.Fatal("still active, want inactive")
	}
}

func skip(r *Recurrence, n int) {
	for range n {
		r.Next()
	}
}

func TestEveryWeekly(t *testing.T) {
	r := mustNew(t, start, "weekly", Options{})
	expectNext(t, r, d(2025, 6, 22))
	if !r.Current().Equal(d(2025, 6, 22)) {
		t.Fatalf("Current() = %s", r.Current())
	}
}

func TestEveryMonthly(t *testing.T) {
	expectNext(t, mustNew(t, start, "monthly", Options{}), d(2025, 7, 15))
}

func TestMonthlyKeepsThe31st(t *testing.T) {
	r := mustNew(t, d(2001, 1, 31), "monthly", Options{})
	expectNext(t, r, d(2001, 2, 28), d(2001, 3, 31), d(2001, 4, 30), d(2001, 5, 31))
}

func TestMonthlyThe30thInLeapYear(t *testing.T) {
	r := mustNew(t, d(2012, 1, 30), "monthly", Options{})
	expectNext(t, r, d(2012, 2, 29), d(2012, 3, 30))
}

func TestYearly(t *testing.T) {
	for _, s := range []string{"yearly", "annually"} {
		expectNext(t, mustNew(t, start, s, Options{}), d(2026, 6, 15))
	}
}

func TestYearlyLeapDay(t *testing.T) {
	r := mustNew(t, d(2012, 2, 29), "yearly", Options{})
	expectNext(t, r, d(2013, 2, 28), d(2014, 2, 28), d(2015, 2, 28), d(2016, 2, 29))
}

func TestEveryNDaysRemembersPriorDate(t *testing.T) {
	r := mustNew(t, start, "every 29 days", Options{})
	skip(r, 3)
	if want := start.AddDate(0, 0, 29*3); !r.Current().Equal(want) {
		t.Fatalf("Current() = %s, want %s", r.Current(), want)
	}
}

func TestEveryNUnits(t *testing.T) {
	cases := []struct {
		interval string
		want     time.Time
	}{
		{"every 2 weeks", start.AddDate(0, 0, 14)},
		{"every 6 years", start.AddDate(6, 0, 0)},
		{"every 10 weeks", start.AddDate(0, 0, 70)},
		{"every 37 months", start.AddDate(0, 37, 0)},
		{"every 19 days", start.AddDate(0, 0, 19)},
		{"every 1 month", start.AddDate(0, 1, 0)},
	}
	for _, c := range cases {
		t.Run(c.interval, func(t *testing.T) {
			expectNext(t, mustNew(t, start, c.interval, Options{}), c.want)
		})
	}
}

func TestEveryZeroIsOneTime(t *testing.T) {
	r := mustNew(t, start, "every 0 months", Options{})
	if !r.Current().Equal(start) {
		t.Fatalf("Current() = %s", r.Current())
	}
	expectDone(t, r)
}

func TestRejectsUnparseable(t *testing.T) {
	for _, s := range []string{"every -1 months", "every 2 fortnights", "whenever", "biweekly"} {
		if _, err := New(start, s, Options{}); err == nil {
			t.Errorf("New(%q) succeeded, want error", s)
		}
	}
}

func TestEveryEndDate(t *testing.T) {
	r := mustNew(t, start, "weekly", Options{End: start.AddDate(0, 0, 8)})
	if !r.Active() {
		t.Fatal("want active")
	}
	expectNext(t, r, start.AddDate(0, 0, 7))
	if !r.Active() {
		t.Fatal("want active")
	}
	expectDone(t, r)
}

// Issue #107 in cashew: monthly and yearly items stepped from the stored
// date, so a date clamped in a short month became the new anchor.
func TestMonthlyFromOriginDoesNotDriftAfterShortMonth(t *testing.T) {
	r := mustNew(t, d(2027, 2, 28), "monthly", Options{Origin: d(2027, 1, 31)})
	expectNext(t, r, d(2027, 3, 31), d(2027, 4, 30), d(2027, 5, 31))
}

func TestMonthlyFromOriginHealsAlreadyDriftedDate(t *testing.T) {
	r := mustNew(t, d(2027, 4, 28), "monthly", Options{Origin: d(2027, 1, 31)})
	expectNext(t, r, d(2027, 4, 30), d(2027, 5, 31))
}

func TestEveryNMonthsFromOrigin(t *testing.T) {
	r := mustNew(t, d(2027, 3, 31), "every 2 months", Options{Origin: d(2027, 1, 31)})
	expectNext(t, r, d(2027, 5, 31), d(2027, 7, 31))
}

func TestYearlyFromOriginLeapDay(t *testing.T) {
	r := mustNew(t, d(2013, 2, 28), "yearly", Options{Origin: d(2012, 2, 29)})
	expectNext(t, r, d(2014, 2, 28), d(2015, 2, 28), d(2016, 2, 29))
}

func TestMonthlyFromVeryOldOrigin(t *testing.T) {
	r := mustNew(t, d(2300, 1, 31), "monthly", Options{Origin: d(1000, 1, 31)})
	expectNext(t, r, d(2300, 2, 28))
}

func TestOriginAfterCurrentIsIgnored(t *testing.T) {
	r := mustNew(t, d(2027, 1, 15), "monthly", Options{Origin: d(2027, 3, 31)})
	expectNext(t, r, d(2027, 2, 15))
}

func TestTwiceAWeek(t *testing.T) {
	r := mustNew(t, d(2013, 1, 1), "2 times a week", Options{})
	expectNext(t, r,
		d(2013, 1, 5), d(2013, 1, 8), d(2013, 1, 12), d(2013, 1, 15), d(2013, 1, 19),
		d(2013, 1, 22), d(2013, 1, 26), d(2013, 1, 29), d(2013, 2, 2), d(2013, 2, 5))
}

// Twice a week doesn't divide evenly: alone it runs 4 days then 3, but an
// earlier origin keeps the 3-then-4 rhythm already under way.
func TestTwiceAWeekEarlierOrigin(t *testing.T) {
	r := mustNew(t, d(2013, 1, 12), "2 times a week", Options{Origin: d(2013, 1, 1)})
	expectNext(t, r, d(2013, 1, 15), d(2013, 1, 19))
}

func TestTwiceAWeekSameOrigin(t *testing.T) {
	r := mustNew(t, d(2013, 1, 1), "2 times a week", Options{Origin: d(2013, 1, 1)})
	expectNext(t, r, d(2013, 1, 5))
}

func TestSevenTimesAWeek(t *testing.T) {
	expectNext(t, mustNew(t, d(2013, 1, 1), "7 times a week", Options{}),
		d(2013, 1, 2), d(2013, 1, 3), d(2013, 1, 4))
}

func TestSevenTimesAWeekEarlierOrigin(t *testing.T) {
	expectNext(t, mustNew(t, d(2013, 1, 2), "7 times a week", Options{Origin: d(2013, 1, 1)}),
		d(2013, 1, 3), d(2013, 1, 4))
}

func TestMoreThanSevenTimesAWeekIsDaily(t *testing.T) {
	expectNext(t, mustNew(t, d(2001, 1, 1), "17 times a week", Options{}),
		d(2001, 1, 2), d(2001, 1, 3), d(2001, 1, 4), d(2001, 1, 5), d(2001, 1, 6), d(2001, 1, 7))
}

// February only yields 7 dates: the spacing stays fixed rather than
// compressing in a short month.
func TestEightTimesAMonthFromThe1st(t *testing.T) {
	r := mustNew(t, d(2001, 1, 1), "8 times a month", Options{})
	expectNext(t, r, d(2001, 1, 5), d(2001, 1, 9), d(2001, 1, 13), d(2001, 1, 17),
		d(2001, 1, 21), d(2001, 1, 25), d(2001, 1, 29), d(2001, 2, 1))
	skip(r, 6)
	expectNext(t, r, d(2001, 3, 1))
}

func TestFiveTimesAMonthFromThe1st(t *testing.T) {
	expectNext(t, mustNew(t, d(2001, 1, 1), "5 times a month", Options{}),
		d(2001, 1, 7), d(2001, 1, 13), d(2001, 1, 19), d(2001, 1, 25),
		d(2001, 2, 1), d(2001, 2, 7), d(2001, 2, 13), d(2001, 2, 19), d(2001, 2, 25),
		d(2001, 3, 1), d(2001, 3, 7))
}

func TestSixTimesAMonthFromThe1st(t *testing.T) {
	expectNext(t, mustNew(t, d(2001, 1, 1), "6 times a month", Options{}),
		d(2001, 1, 6), d(2001, 1, 11), d(2001, 1, 16), d(2001, 1, 21), d(2001, 1, 26),
		d(2001, 2, 1), d(2001, 2, 6), d(2001, 2, 11), d(2001, 2, 16), d(2001, 2, 21), d(2001, 2, 26),
		d(2001, 3, 1))
}

func TestEightTimesAMonthFromThe31st(t *testing.T) {
	r := mustNew(t, d(2001, 1, 31), "8 times a month", Options{})
	expectNext(t, r, d(2001, 2, 4), d(2001, 2, 8))
	skip(r, 4)
	expectNext(t, r, d(2001, 2, 28), d(2001, 3, 4))
	skip(r, 6)
	expectNext(t, r, d(2001, 3, 31), d(2001, 4, 4))
	skip(r, 6)
	expectNext(t, r, d(2001, 4, 30), d(2001, 5, 4))
}

func TestTwiceAMonth(t *testing.T) {
	expectNext(t, mustNew(t, d(2001, 1, 1), "2 times a month", Options{}),
		d(2001, 1, 16), d(2001, 2, 1), d(2001, 2, 16), d(2001, 3, 1))
}

func TestTimesWithVeryOldOrigin(t *testing.T) {
	expectNext(t, mustNew(t, d(2300, 1, 1), "7 times a week", Options{Origin: d(1000, 1, 1)}),
		d(2300, 1, 2))
}

func TestTimesEndDate(t *testing.T) {
	r := mustNew(t, d(2013, 1, 1), "2 times a week", Options{End: d(2013, 1, 9)})
	if !r.Active() {
		t.Fatal("want active")
	}
	expectNext(t, r, d(2013, 1, 5), d(2013, 1, 8))
	if !r.Active() {
		t.Fatal("want active")
	}
	expectDone(t, r)
}

// A real bug: catching up from an origin two months back needed more
// internal dates loaded.
func TestTwiceAMonthOriginTwoMonthsBehind(t *testing.T) {
	r := mustNew(t, d(2017, 3, 1), "2 times a month", Options{End: d(2017, 8, 16), Origin: d(2017, 1, 1)})
	expectNext(t, r, d(2017, 3, 16))
}

func TestNeverAndOnce(t *testing.T) {
	for _, s := range []string{"never", "once"} {
		t.Run(s, func(t *testing.T) {
			r := mustNew(t, start, s, Options{})
			if !r.Active() {
				t.Fatal("want active")
			}
			if !r.Current().Equal(start) {
				t.Fatalf("Current() = %s", r.Current())
			}
			expectDone(t, r)
		})
	}
}
