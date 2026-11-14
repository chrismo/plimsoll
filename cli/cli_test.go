package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chrismo/plimsoll/plan"
	"github.com/chrismo/plimsoll/store"
)

var today = date("2026-11-04")

func date(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

type result struct {
	out, err string
	code     int
}

func plim(t *testing.T, dir string, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	app := &App{Stdout: &out, Stderr: &errOut, Today: today}
	code := app.Run(append([]string{"--dir", dir}, args...))
	return result{out.String(), errOut.String(), code}
}

func ok(t *testing.T, r result) result {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", r.code, r.out, r.err)
	}
	return r
}

func fails(t *testing.T, r result, code int, mention string) {
	t.Helper()
	if r.code != code || !strings.Contains(r.err, mention) {
		t.Errorf("want exit %d mentioning %q, got exit %d\nstderr: %s", code, mention, r.code, r.err)
	}
}

func seed(t *testing.T, accounts ...plan.Account) string {
	t.Helper()
	dir := t.TempDir()
	if err := store.Save(dir, accounts); err != nil {
		t.Fatal(err)
	}
	return dir
}

func load(t *testing.T, dir string) []plan.Account {
	t.Helper()
	accounts, err := store.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return accounts
}

func checking() plan.Account {
	return plan.Account{Name: "checking", Balance: 250000, AsOf: date("2026-11-01"), Items: []plan.Item{
		{ID: "rent", Description: "Rent", Amount: -100000, Date: date("2026-11-02"), Origin: date("2026-11-02"), Recurrence: "monthly"},
		{ID: "coffee", Description: "Coffee", Amount: -500, Date: date("2026-11-03"), Origin: date("2026-11-03"), Recurrence: "weekly"},
		{ID: "pay", Description: "Paycheck", Amount: 200000, Date: date("2026-11-13"), Origin: date("2026-11-13"), Recurrence: "every 2 weeks"},
	}}
}

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	return v
}

func assertJSON(t *testing.T, got string, want string) {
	t.Helper()
	if g, w := decode(t, got), decode(t, want); !reflect.DeepEqual(g, w) {
		t.Errorf("JSON mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestOpenThenAccounts(t *testing.T) {
	dir := t.TempDir()

	ok(t, plim(t, dir, "open", "checking", "--balance", "2500.00", "--as-of", "2026-11-01"))
	ok(t, plim(t, dir, "open", "savings"))

	assertJSON(t, ok(t, plim(t, dir, "accounts", "--json")).out, `{"accounts":[
		{"name":"checking","balance":"2500.00","as_of":"2026-11-01"},
		{"name":"savings","balance":"0.00","as_of":"2026-11-04"}]}`)
	if out := ok(t, plim(t, dir, "accounts")).out; !strings.Contains(out, "checking") || !strings.Contains(out, "2500.00") {
		t.Errorf("text output missing account: %s", out)
	}
}

func TestOpenRefusesDuplicate(t *testing.T) {
	dir := seed(t, checking())

	fails(t, plim(t, dir, "open", "checking"), 1, `"checking" already exists`)
}

func TestNoAccountsSaysHowToStart(t *testing.T) {
	fails(t, plim(t, t.TempDir(), "ahead"), 1, "plim open")
}

func TestAccountRequiredWhenSeveral(t *testing.T) {
	dir := seed(t, checking(), plan.Account{Name: "savings", AsOf: today})

	fails(t, plim(t, dir, "ahead"), 1, "checking, savings")
	ok(t, plim(t, dir, "ahead", "--account", "savings"))
	fails(t, plim(t, dir, "ahead", "--account", "nope"), 1, `"nope"`)
}

func TestBalanceSetsAmountAndDate(t *testing.T) {
	dir := seed(t, checking())

	ok(t, plim(t, dir, "balance", "--amount", "-12.34"))

	a := load(t, dir)[0]
	if a.Balance != -1234 || !a.AsOf.Equal(today) {
		t.Errorf("got %s as of %s", a.Balance, a.AsOf.Format(time.DateOnly))
	}

	ok(t, plim(t, dir, "balance", "--amount", "100", "--date", "2026-11-03"))
	if a := load(t, dir)[0]; a.Balance != 10000 || !a.AsOf.Equal(date("2026-11-03")) {
		t.Errorf("got %s as of %s", a.Balance, a.AsOf.Format(time.DateOnly))
	}
}

func TestAddCreatesItemWithReadableID(t *testing.T) {
	dir := seed(t, checking())

	r := ok(t, plim(t, dir, "add", "--desc", "Car Insurance", "--amount", "-120.50", "--date", "2026-11-20", "--repeat", "every 6 months", "--json"))

	assertJSON(t, r.out, `{"item":{"id":"car-insurance","description":"Car Insurance","amount":"-120.50",
		"date":"2026-11-20","repeat":"every 6 months","end":null}}`)
	items := load(t, dir)[0].Items
	got := items[len(items)-1]
	want := plan.Item{
		ID: "car-insurance", Description: "Car Insurance", Amount: -12050,
		Date: date("2026-11-20"), Origin: date("2026-11-20"), Recurrence: "every 6 months",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestAddDefaultsToOnceAndDedupesID(t *testing.T) {
	dir := seed(t, checking())

	ok(t, plim(t, dir, "add", "--desc", "Rent", "--amount", "-50", "--date", "2026-11-09"))

	items := load(t, dir)[0].Items
	got := items[len(items)-1]
	if got.ID != "rent-2" || got.Recurrence != "once" {
		t.Errorf("got %+v", got)
	}
}

func TestAddRejectsBadInput(t *testing.T) {
	dir := seed(t, checking())
	base := []string{"add", "--desc", "Gym", "--amount", "-25", "--date", "2026-11-09"}

	fails(t, plim(t, dir, append(base, "--repeat", "biweekly")...), 1, "biweekly")
	fails(t, plim(t, dir, "add", "--desc", "Gym", "--amount", "$25", "--date", "2026-11-09"), 1, "$25")
	fails(t, plim(t, dir, "add", "--amount", "-25", "--date", "2026-11-09"), 2, "--desc")
	fails(t, plim(t, dir, append(base, "--id", "rent")...), 1, `"rent" already exists`)
	fails(t, plim(t, dir, append(base, "--end", "2026-11-01")...), 1, "end")
	if n := len(load(t, dir)[0].Items); n != 3 {
		t.Errorf("rejected adds should save nothing, have %d items", n)
	}
}

func TestAheadJSON(t *testing.T) {
	dir := seed(t, checking())

	r := ok(t, plim(t, dir, "ahead", "--through", "2026-11-13", "--json"))

	assertJSON(t, r.out, `{
		"account":"checking","balance":"2500.00","as_of":"2026-11-01","through":"2026-11-13",
		"days":[
			{"date":"2026-11-01","balance":"2500.00","rows":[]},
			{"date":"2026-11-02","balance":"1500.00","rows":[
				{"id":"rent","description":"Rent","amount":"-1000.00","repeat":"monthly"}]},
			{"date":"2026-11-03","balance":"1495.00","rows":[
				{"id":"coffee","description":"Coffee","amount":"-5.00","repeat":"weekly"}]},
			{"date":"2026-11-10","balance":"1490.00","rows":[
				{"id":"coffee","description":"Coffee","amount":"-5.00","repeat":"weekly"}]},
			{"date":"2026-11-13","balance":"3490.00","rows":[
				{"id":"pay","description":"Paycheck","amount":"2000.00","repeat":"every 2 weeks"}]}
		],
		"low":{"date":"2026-11-10","balance":"1490.00"}}`)
}

func TestAheadText(t *testing.T) {
	dir := seed(t, checking())

	out := ok(t, plim(t, dir, "ahead", "--through", "2026-11-13")).out

	for _, want := range []string{"checking", "2026-11-02", "rent", "-1000.00", "1500.00", "low 1490.00 on 2026-11-10"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestAheadDefaultsToSixMonths(t *testing.T) {
	dir := seed(t, checking())

	out := ok(t, plim(t, dir, "ahead", "--json")).out

	if got := decode(t, out).(map[string]any)["through"]; got != "2027-05-04" {
		t.Errorf("through: got %v, want six months from today", got)
	}
}

func TestPayAppliesAndReportsNextDates(t *testing.T) {
	dir := seed(t, checking())

	r := ok(t, plim(t, dir, "pay", "rent@2026-11-02", "coffee@2026-11-03", "--json"))

	assertJSON(t, r.out, `{"items":[
		{"id":"rent","cleared":false,"next_date":"2026-12-02"},
		{"id":"coffee","cleared":false,"next_date":"2026-11-10"}],
		"balance":{"amount":"1495.00","as_of":"2026-11-04"}}`)
	a := load(t, dir)[0]
	if a.Balance != 149500 {
		t.Errorf("saved balance %s", a.Balance)
	}
}

func TestPayMismatchSavesNothing(t *testing.T) {
	dir := seed(t, checking())

	fails(t, plim(t, dir, "pay", "rent@2026-11-02", "coffee@2026-11-10"), 1, "2026-11-03")

	if !reflect.DeepEqual(load(t, dir), []plan.Account{checking()}) {
		t.Errorf("data changed after a refused pay")
	}
}

func TestPayNeedsAnItem(t *testing.T) {
	fails(t, plim(t, seed(t, checking()), "pay"), 2, "item")
}

func TestPayBadExpectedDate(t *testing.T) {
	fails(t, plim(t, seed(t, checking()), "pay", "rent@tuesday"), 1, "tuesday")
}

func TestPayBareIDWhenDue(t *testing.T) {
	dir := seed(t, checking())

	ok(t, plim(t, dir, "pay", "rent"))

	if a := load(t, dir)[0]; a.Balance != 150000 {
		t.Errorf("saved balance %s", a.Balance)
	}
}

func TestPayBareIDNotYetDueNamesTheDatedForm(t *testing.T) {
	dir := seed(t, checking())

	fails(t, plim(t, dir, "pay", "pay"), 1, "pay@2026-11-13")

	if !reflect.DeepEqual(load(t, dir), []plan.Account{checking()}) {
		t.Errorf("data changed after a refused pay")
	}
}

func TestSkipLeavesBalance(t *testing.T) {
	dir := seed(t, checking())

	ok(t, plim(t, dir, "skip", "coffee@2026-11-03"))

	a := load(t, dir)[0]
	if a.Balance != 250000 || !a.Items[1].Date.Equal(date("2026-11-10")) {
		t.Errorf("got balance %s, coffee %s", a.Balance, a.Items[1].Date.Format(time.DateOnly))
	}
}

func TestAffordStaysAboveFloor(t *testing.T) {
	dir := seed(t, checking())

	r := ok(t, plim(t, dir, "afford", "400", "--date", "2026-11-05", "--through", "2026-11-13", "--json"))

	assertJSON(t, r.out, `{"affordable":true,"cost":"400.00","date":"2026-11-05","floor":"0.00",
		"low_before":{"date":"2026-11-10","balance":"1490.00"},
		"low_after":{"date":"2026-11-10","balance":"1090.00"}}`)
	if load(t, dir)[0].Items[0].ID != "rent" || len(load(t, dir)[0].Items) != 3 {
		t.Error("afford must not save the hypothetical item")
	}
}

func TestAffordBelowFloor(t *testing.T) {
	dir := seed(t, checking())

	r := ok(t, plim(t, dir, "afford", "400", "--date", "2026-11-05", "--floor", "1200", "--through", "2026-11-13"))

	if !strings.HasPrefix(r.out, "no") || !strings.Contains(r.out, "1090.00") {
		t.Errorf("got:\n%s", r.out)
	}
}

func TestUnknownCommand(t *testing.T) {
	fails(t, plim(t, t.TempDir(), "frobnicate"), 2, "frobnicate")
}

func TestEditChangesAmountWithGuards(t *testing.T) {
	dir := seed(t, checking())

	r := ok(t, plim(t, dir, "edit", "rent@2026-11-02", "--amount", "-1100", "--expected-amount", "-1000", "--json"))

	assertJSON(t, r.out, `{"item":{"id":"rent","description":"Rent","amount":"-1100.00",
		"date":"2026-11-02","repeat":"monthly","end":null}}`)
	if got := load(t, dir)[0].Items[0].Amount; got != -110000 {
		t.Errorf("saved amount %s", got)
	}
}

func TestEditSetsAndClearsEnd(t *testing.T) {
	dir := seed(t, checking())

	ok(t, plim(t, dir, "edit", "coffee@2026-11-03", "--end", "2026-12-31", "--desc", "Latte"))
	it := load(t, dir)[0].Items[1]
	if it.Description != "Latte" || !it.End.Equal(date("2026-12-31")) {
		t.Errorf("got %+v", it)
	}

	ok(t, plim(t, dir, "edit", "coffee@2026-11-03", "--no-end"))
	if it := load(t, dir)[0].Items[1]; !it.End.IsZero() {
		t.Errorf("end not cleared: %+v", it)
	}
}

func TestEditMovesDateAndRepeat(t *testing.T) {
	dir := seed(t, checking())

	ok(t, plim(t, dir, "edit", "pay@2026-11-13", "--date", "2026-11-14", "--repeat", "2 times a month"))

	it := load(t, dir)[0].Items[2]
	if !it.Date.Equal(date("2026-11-14")) || !it.Origin.Equal(date("2026-11-14")) || it.Recurrence != "2 times a month" {
		t.Errorf("got %+v", it)
	}
}

func TestEditRefusals(t *testing.T) {
	dir := seed(t, checking())

	fails(t, plim(t, dir, "edit", "rent", "--amount", "1"), 2, "ID@EXPECTED-DATE")
	fails(t, plim(t, dir, "edit", "rent@2026-11-02"), 2, "nothing to change")
	fails(t, plim(t, dir, "edit", "rent@2026-11-02", "--end", "2026-12-31", "--no-end"), 2, "--no-end")
	fails(t, plim(t, dir, "edit", "rent@2026-11-02", "rent@2026-11-02", "--amount", "1"), 2, "one item")
	fails(t, plim(t, dir, "edit", "rent@2026-11-02", "--amount", "1", "--expected-amount", "-999"), 1, "-1000.00")
	fails(t, plim(t, dir, "edit", "rent@2026-12-02", "--amount", "1"), 1, "2026-11-02")
	fails(t, plim(t, dir, "edit", "rent@2026-11-02", "--repeat", "biweekly"), 1, "biweekly")

	if !reflect.DeepEqual(load(t, dir), []plan.Account{checking()}) {
		t.Errorf("data changed after refused edits")
	}
}

func TestAddRefusesDuplicateUnlessAllowed(t *testing.T) {
	dir := seed(t, checking())
	rent := []string{"add", "--desc", "Rent", "--amount", "-1000", "--date", "2026-11-02", "--repeat", "monthly"}

	fails(t, plim(t, dir, rent...), 1, `"rent"`)
	if n := len(load(t, dir)[0].Items); n != 3 {
		t.Fatalf("duplicate was saved: %d items", n)
	}

	r := ok(t, plim(t, dir, append(rent, "--allow-duplicate")...))
	if !strings.Contains(r.out, "rent-2") || len(load(t, dir)[0].Items) != 4 {
		t.Errorf("want rent-2 added, got %s", r.out)
	}
}

type brokenPipe struct{}

func (brokenPipe) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestFailedStdoutWriteExitsNonZero(t *testing.T) {
	var errOut bytes.Buffer
	app := &App{Stdout: brokenPipe{}, Stderr: &errOut, Today: today}

	code := app.Run([]string{"--dir", seed(t, checking()), "ahead"})

	if code != 1 || !strings.Contains(errOut.String(), "broken pipe") {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}
