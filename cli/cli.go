// Package cli implements the plim command.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/chrismo/plimsoll/plan"
	"github.com/chrismo/plimsoll/recur"
	"github.com/chrismo/plimsoll/store"
)

type App struct {
	Stdout, Stderr io.Writer
	Today          time.Time
}

const usage = `usage: plim [--dir DIR] COMMAND [flags]

Data lives in --dir, else $PLIM_DIR, else $XDG_DATA_HOME/plim
(~/.local/share/plim). Dates are YYYY-MM-DD. Amounts are plain decimals,
negative for money going out: -1234.56. Every command takes --json, and
all but open take --account when there is more than one account.

commands:
  accounts                          list accounts and balances
  open NAME [--balance N] [--as-of D]
                                    create an account
  balance --amount N [--date D]     set the current balance
  add --desc S --amount N --date D [--repeat R] [--end D] [--id ID]
      [--allow-duplicate]
                                    plan an item; R is once (default),
                                    weekly, monthly, yearly, every 2 weeks,
                                    every 3 months, 2 times a month, ...
                                    An exact repeat of an existing item is
                                    refused unless --allow-duplicate.
  edit ID@EXPECTED-DATE [--desc S] [--amount N] [--date D] [--repeat R]
       [--end D | --no-end] [--expected-amount N]
                                    change an item; --date moves its next
                                    occurrence and later ones follow
  ahead [--through D]               daily balances and the low point
                                    (default through: 6 months out)
  pay ID[@EXPECTED-DATE] ...        apply items to the balance and advance
                                    them; all or nothing. A bare ID only
                                    acts on an item due by today.
  skip ID[@EXPECTED-DATE] ...       advance items without paying
  afford COST [--date D] [--floor N] [--through D]
                                    would a one-time cost keep the low
                                    point at or above the floor (0)?
`

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error { return usageError{fmt.Sprintf(format, args...)} }

type command func(c *ctx, args []string) error

var commands = map[string]command{
	"accounts": accountsCmd,
	"open":     openCmd,
	"balance":  balanceCmd,
	"add":      addCmd,
	"edit":     editCmd,
	"ahead":    aheadCmd,
	"pay":      func(c *ctx, args []string) error { return advanceCmd(c, args, "pay") },
	"skip":     func(c *ctx, args []string) error { return advanceCmd(c, args, "skip") },
	"afford":   affordCmd,
}

func (a *App) Run(args []string) int {
	global := flag.NewFlagSet("plim", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	dirFlag := global.String("dir", "", "")
	if err := global.Parse(args); err != nil || global.NArg() == 0 {
		fmt.Fprint(a.Stderr, usage)
		return 2
	}
	name := global.Arg(0)
	cmd, found := commands[name]
	if !found {
		fmt.Fprintf(a.Stderr, "plim: unknown command %q\n\n%s", name, usage)
		return 2
	}
	dir, err := store.Dir(*dirFlag)
	if err == nil {
		err = cmd(&ctx{App: a, dir: dir, name: name}, global.Args()[1:])
	}
	if err != nil {
		fmt.Fprintf(a.Stderr, "plim %s: %v\n", name, err)
		if errors.As(err, new(usageError)) {
			return 2
		}
		return 1
	}
	return 0
}

type ctx struct {
	*App
	dir  string
	name string
	json bool
}

func (c *ctx) flags() *flag.FlagSet {
	fs := flag.NewFlagSet(c.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&c.json, "json", false, "")
	return fs
}

// parse lets positional arguments and flags come in any order.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, usageError{err.Error()}
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func (c *ctx) load() ([]plan.Account, error) {
	return store.Load(c.dir)
}

func (c *ctx) pick(accounts []plan.Account, name string) (*plan.Account, error) {
	if len(accounts) == 0 {
		return nil, errors.New("no accounts yet; create one with: plim open NAME")
	}
	var names []string
	for i := range accounts {
		if accounts[i].Name == name || (name == "" && len(accounts) == 1) {
			return &accounts[i], nil
		}
		names = append(names, accounts[i].Name)
	}
	if name == "" {
		return nil, fmt.Errorf("choose an account with --account: %s", strings.Join(names, ", "))
	}
	return nil, fmt.Errorf("no account %q; have: %s", name, strings.Join(names, ", "))
}

func (c *ctx) output(v any, text func(w io.Writer)) error {
	if !c.json {
		text(c.Stdout)
		return nil
	}
	enc := json.NewEncoder(c.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (c *ctx) dateOr(s string, fallback time.Time) (time.Time, error) {
	if s == "" {
		return fallback, nil
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return d, nil
}

func day(t time.Time) string { return t.Format(time.DateOnly) }

func parseRef(s string) (plan.Ref, error) {
	id, expected, hasDate := strings.Cut(s, "@")
	ref := plan.Ref{ID: id}
	if hasDate {
		var err error
		if ref.Expected, err = time.Parse(time.DateOnly, expected); err != nil {
			return ref, fmt.Errorf("%s: invalid expected date %q: want YYYY-MM-DD", s, expected)
		}
	}
	return ref, nil
}

type accountJSON struct {
	Name    string `json:"name"`
	Balance string `json:"balance"`
	AsOf    string `json:"as_of"`
}

func accountsCmd(c *ctx, args []string) error {
	fs := c.flags()
	if _, err := parse(fs, args); err != nil {
		return err
	}
	accounts, err := c.load()
	if err != nil {
		return err
	}
	list := []accountJSON{}
	for _, a := range accounts {
		list = append(list, accountJSON{a.Name, a.Balance.String(), day(a.AsOf)})
	}
	return c.output(map[string]any{"accounts": list}, func(w io.Writer) {
		for _, a := range list {
			fmt.Fprintf(w, "%-20s %12s  as of %s\n", a.Name, a.Balance, a.AsOf)
		}
	})
}

func openCmd(c *ctx, args []string) error {
	fs := c.flags()
	balance := fs.String("balance", "0", "")
	asOf := fs.String("as-of", "", "")
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return usagef("give one account name")
	}
	name := positional[0]
	cents, err := plan.ParseCents(*balance)
	if err != nil {
		return err
	}
	date, err := c.dateOr(*asOf, c.Today)
	if err != nil {
		return err
	}
	accounts, err := c.load()
	if err != nil {
		return err
	}
	if slices.ContainsFunc(accounts, func(a plan.Account) bool { return a.Name == name }) {
		return fmt.Errorf("account %q already exists", name)
	}
	a := plan.Account{Name: name, Balance: cents, AsOf: date}
	if err := store.Save(c.dir, append(accounts, a)); err != nil {
		return err
	}
	out := accountJSON{a.Name, a.Balance.String(), day(a.AsOf)}
	return c.output(map[string]any{"account": out}, func(w io.Writer) {
		fmt.Fprintf(w, "opened %s: %s as of %s\n", out.Name, out.Balance, out.AsOf)
	})
}

func balanceCmd(c *ctx, args []string) error {
	fs := c.flags()
	account := fs.String("account", "", "")
	amount := fs.String("amount", "", "")
	dateFlag := fs.String("date", "", "")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if *amount == "" {
		return usagef("--amount is required")
	}
	cents, err := plan.ParseCents(*amount)
	if err != nil {
		return err
	}
	date, err := c.dateOr(*dateFlag, c.Today)
	if err != nil {
		return err
	}
	accounts, err := c.load()
	if err != nil {
		return err
	}
	a, err := c.pick(accounts, *account)
	if err != nil {
		return err
	}
	a.Balance, a.AsOf = cents, date
	if err := store.Save(c.dir, accounts); err != nil {
		return err
	}
	out := accountJSON{a.Name, a.Balance.String(), day(a.AsOf)}
	return c.output(map[string]any{"account": out}, func(w io.Writer) {
		fmt.Fprintf(w, "%s: %s as of %s\n", out.Name, out.Balance, out.AsOf)
	})
}

type itemJSON struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	Amount      string  `json:"amount"`
	Date        string  `json:"date"`
	Repeat      string  `json:"repeat"`
	End         *string `json:"end"`
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 32 {
		s = strings.TrimRight(s[:32], "-")
	}
	if s == "" {
		return "item"
	}
	return s
}

func addCmd(c *ctx, args []string) error {
	fs := c.flags()
	account := fs.String("account", "", "")
	desc := fs.String("desc", "", "")
	amount := fs.String("amount", "", "")
	dateFlag := fs.String("date", "", "")
	repeat := fs.String("repeat", "once", "")
	endFlag := fs.String("end", "", "")
	id := fs.String("id", "", "")
	allowDuplicate := fs.Bool("allow-duplicate", false, "")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	for flagName, v := range map[string]string{"--desc": *desc, "--amount": *amount, "--date": *dateFlag} {
		if v == "" {
			return usagef("%s is required", flagName)
		}
	}
	cents, err := plan.ParseCents(*amount)
	if err != nil {
		return err
	}
	date, err := c.dateOr(*dateFlag, time.Time{})
	if err != nil {
		return err
	}
	end, err := c.dateOr(*endFlag, time.Time{})
	if err != nil {
		return err
	}
	if !end.IsZero() && end.Before(date) {
		return fmt.Errorf("--end %s is before --date %s", day(end), day(date))
	}
	if _, err := recur.New(date, *repeat, recur.Options{}); err != nil {
		return err
	}
	accounts, err := c.load()
	if err != nil {
		return err
	}
	a, err := c.pick(accounts, *account)
	if err != nil {
		return err
	}
	taken := map[string]bool{}
	for _, acct := range accounts {
		for _, it := range acct.Items {
			taken[it.ID] = true
		}
	}
	if *id != "" && taken[*id] {
		return fmt.Errorf("id %q already exists", *id)
	}
	if *id == "" {
		*id = slug(*desc)
		for n := 2; taken[*id]; n++ {
			*id = fmt.Sprintf("%s-%d", slug(*desc), n)
		}
	}
	it := plan.Item{ID: *id, Description: *desc, Amount: cents, Date: date, Origin: date, End: end, Recurrence: *repeat}
	if err := a.Add(it, *allowDuplicate); err != nil {
		if errors.As(err, new(*plan.Duplicate)) {
			return fmt.Errorf("%w; pass --allow-duplicate to add it anyway", err)
		}
		return err
	}
	if err := store.Save(c.dir, accounts); err != nil {
		return err
	}
	return c.outputItem("added", it)
}

func (c *ctx) outputItem(verb string, it plan.Item) error {
	out := itemJSON{ID: it.ID, Description: it.Description, Amount: it.Amount.String(), Date: day(it.Date), Repeat: it.Recurrence}
	if !it.End.IsZero() {
		e := day(it.End)
		out.End = &e
	}
	return c.output(map[string]any{"item": out}, func(w io.Writer) {
		fmt.Fprintf(w, "%s %s: %s %s on %s, %s", verb, out.ID, out.Description, out.Amount, out.Date, out.Repeat)
		if out.End != nil {
			fmt.Fprintf(w, ", until %s", *out.End)
		}
		fmt.Fprintln(w)
	})
}

func editCmd(c *ctx, args []string) error {
	fs := c.flags()
	account := fs.String("account", "", "")
	desc := fs.String("desc", "", "")
	amount := fs.String("amount", "", "")
	dateFlag := fs.String("date", "", "")
	repeat := fs.String("repeat", "", "")
	endFlag := fs.String("end", "", "")
	noEnd := fs.Bool("no-end", false, "")
	expectedAmount := fs.String("expected-amount", "", "")
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return usagef("name one item, as ID@EXPECTED-DATE")
	}
	ref, err := parseRef(positional[0])
	if err != nil {
		return err
	}
	if ref.Expected.IsZero() {
		return usagef("name the item as ID@EXPECTED-DATE; edit always checks the next date")
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["end"] && set["no-end"] {
		return usagef("give --end or --no-end, not both")
	}
	var ch plan.Change
	if set["desc"] {
		ch.Description = desc
	}
	if set["amount"] {
		cents, err := plan.ParseCents(*amount)
		if err != nil {
			return err
		}
		ch.Amount = &cents
	}
	if set["date"] {
		d, err := c.dateOr(*dateFlag, time.Time{})
		if err != nil {
			return err
		}
		ch.Date = &d
	}
	if set["repeat"] {
		ch.Recurrence = repeat
	}
	if set["end"] {
		d, err := c.dateOr(*endFlag, time.Time{})
		if err != nil {
			return err
		}
		ch.End = &d
	}
	if *noEnd {
		ch.End = &time.Time{}
	}
	if ch == (plan.Change{}) {
		return usagef("nothing to change")
	}
	var guard *plan.Cents
	if set["expected-amount"] {
		cents, err := plan.ParseCents(*expectedAmount)
		if err != nil {
			return err
		}
		guard = &cents
	}

	accounts, err := c.load()
	if err != nil {
		return err
	}
	a, err := c.pick(accounts, *account)
	if err != nil {
		return err
	}
	it, err := a.Edit(ref, guard, ch)
	if err != nil {
		return err
	}
	if err := store.Save(c.dir, accounts); err != nil {
		return err
	}
	return c.outputItem("edited", it)
}

type rowJSON struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Amount      string `json:"amount"`
	Repeat      string `json:"repeat"`
}

type dayJSON struct {
	Date    string    `json:"date"`
	Balance string    `json:"balance"`
	Rows    []rowJSON `json:"rows"`
}

type lowJSON struct {
	Date    string `json:"date"`
	Balance string `json:"balance"`
}

func lowOf(days []plan.Day) lowJSON {
	low, _ := plan.Low(days)
	return lowJSON{day(low.Date), low.Balance.String()}
}

func aheadCmd(c *ctx, args []string) error {
	fs := c.flags()
	account := fs.String("account", "", "")
	throughFlag := fs.String("through", "", "")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	through, err := c.dateOr(*throughFlag, c.Today.AddDate(0, 6, 0))
	if err != nil {
		return err
	}
	accounts, err := c.load()
	if err != nil {
		return err
	}
	a, err := c.pick(accounts, *account)
	if err != nil {
		return err
	}
	days, err := a.Forecast(through)
	if err != nil {
		return err
	}
	var out []dayJSON
	for _, d := range days {
		dj := dayJSON{Date: day(d.Date), Balance: d.Balance.String(), Rows: []rowJSON{}}
		for _, o := range d.Occurrences {
			dj.Rows = append(dj.Rows, rowJSON{o.Item.ID, o.Item.Description, o.Amount.String(), o.Item.Recurrence})
		}
		out = append(out, dj)
	}
	low := lowOf(days)
	v := map[string]any{
		"account": a.Name, "balance": a.Balance.String(), "as_of": day(a.AsOf), "through": day(through),
		"days": out, "low": low,
	}
	return c.output(v, func(w io.Writer) {
		fmt.Fprintf(w, "%s: %s as of %s, through %s\n\n", a.Name, a.Balance, day(a.AsOf), day(through))
		idW, descW := 0, 0
		for _, d := range out {
			for _, r := range d.Rows {
				idW, descW = max(idW, len(r.ID)), max(descW, len(r.Description))
			}
		}
		for _, d := range out {
			if len(d.Rows) == 0 {
				fmt.Fprintf(w, "%s  %-*s  %-*s  %10s  %10s\n", d.Date, idW, "", descW, "", "", d.Balance)
			}
			for i, r := range d.Rows {
				balance := ""
				if i == len(d.Rows)-1 {
					balance = d.Balance
				}
				fmt.Fprintf(w, "%s  %-*s  %-*s  %10s  %10s\n", d.Date, idW, r.ID, descW, r.Description, r.Amount, balance)
			}
		}
		fmt.Fprintf(w, "\nlow %s on %s\n", low.Balance, low.Date)
	})
}

type nextJSON struct {
	ID       string  `json:"id"`
	Cleared  bool    `json:"cleared"`
	NextDate *string `json:"next_date"`
}

func advanceCmd(c *ctx, args []string, verb string) error {
	fs := c.flags()
	account := fs.String("account", "", "")
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return usagef("name at least one item, as ID or ID@EXPECTED-DATE")
	}
	var refs []plan.Ref
	var ids []string
	for _, p := range positional {
		ref, err := parseRef(p)
		if err != nil {
			return err
		}
		refs = append(refs, ref)
		if !slices.Contains(ids, ref.ID) {
			ids = append(ids, ref.ID)
		}
	}
	accounts, err := c.load()
	if err != nil {
		return err
	}
	a, err := c.pick(accounts, *account)
	if err != nil {
		return err
	}
	if verb == "pay" {
		err = a.Pay(c.Today, refs...)
	} else {
		err = a.Skip(c.Today, refs...)
	}
	if notDue := (*plan.NotDue)(nil); errors.As(err, &notDue) {
		return fmt.Errorf("%w; to %s it early, name the date: %s@%s", err, verb, notDue.ID, day(notDue.Next))
	}
	if err != nil {
		return err
	}
	if err := store.Save(c.dir, accounts); err != nil {
		return err
	}
	var items []nextJSON
	for _, id := range ids {
		n := nextJSON{ID: id, Cleared: true}
		if i := slices.IndexFunc(a.Items, func(it plan.Item) bool { return it.ID == id }); i >= 0 {
			d := day(a.Items[i].Date)
			n = nextJSON{ID: id, NextDate: &d}
		}
		items = append(items, n)
	}
	balance := map[string]string{"amount": a.Balance.String(), "as_of": day(a.AsOf)}
	return c.output(map[string]any{"items": items, "balance": balance}, func(w io.Writer) {
		for _, n := range items {
			if n.Cleared {
				fmt.Fprintf(w, "%s %s: cleared\n", verb, n.ID)
			} else {
				fmt.Fprintf(w, "%s %s: next %s\n", verb, n.ID, *n.NextDate)
			}
		}
		fmt.Fprintf(w, "balance %s as of %s\n", balance["amount"], balance["as_of"])
	})
}

func affordCmd(c *ctx, args []string) error {
	fs := c.flags()
	account := fs.String("account", "", "")
	dateFlag := fs.String("date", "", "")
	floorFlag := fs.String("floor", "0", "")
	throughFlag := fs.String("through", "", "")
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return usagef("give one cost, as a positive amount")
	}
	cost, err := plan.ParseCents(positional[0])
	if err != nil {
		return err
	}
	floor, err := plan.ParseCents(*floorFlag)
	if err != nil {
		return err
	}
	date, err := c.dateOr(*dateFlag, c.Today)
	if err != nil {
		return err
	}
	through, err := c.dateOr(*throughFlag, c.Today.AddDate(0, 6, 0))
	if err != nil {
		return err
	}
	accounts, err := c.load()
	if err != nil {
		return err
	}
	a, err := c.pick(accounts, *account)
	if err != nil {
		return err
	}
	before, err := a.Forecast(through)
	if err != nil {
		return err
	}
	what := *a
	what.Items = append(slices.Clone(a.Items), plan.Item{Description: "afford", Amount: -cost, Date: date, Recurrence: "once"})
	after, err := what.Forecast(through)
	if err != nil {
		return err
	}
	lowAfter, _ := plan.Low(after)
	affordable := lowAfter.Balance >= floor
	v := map[string]any{
		"affordable": affordable, "cost": cost.String(), "date": day(date), "floor": floor.String(),
		"low_before": lowOf(before), "low_after": lowOf(after),
	}
	return c.output(v, func(w io.Writer) {
		verdict, relation := "yes", "stays at or above"
		if !affordable {
			verdict, relation = "no", "drops below"
		}
		fmt.Fprintf(w, "%s: spending %s on %s, the low point %s floor %s\n", verdict, cost, day(date), relation, floor)
		fmt.Fprintf(w, "low before %s on %s\n", lowOf(before).Balance, lowOf(before).Date)
		fmt.Fprintf(w, "low after  %s on %s\n", lowOf(after).Balance, lowOf(after).Date)
	})
}
