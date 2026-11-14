// Package store reads and writes plim's data folder: accounts.sup and
// items.sup, one record per line, joined on the account name.
package store

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/superdb/super"
	"github.com/superdb/super/sio/supio"

	"github.com/chrismo/plimsoll/plan"
	"github.com/chrismo/plimsoll/recur"
)

const (
	accountsFile = "accounts.sup"
	itemsFile    = "items.sup"
)

type accountRecord struct {
	Name         string `super:"name"`
	BalanceCents int64  `super:"balance_cents"`
	AsOf         string `super:"as_of"`
}

type itemRecord struct {
	ID          string  `super:"id"`
	Account     string  `super:"account"`
	Description string  `super:"description"`
	AmountCents int64   `super:"amount_cents"`
	Date        string  `super:"date"`
	Origin      *string `super:"origin"`
	End         *string `super:"end"`
	Recurrence  string  `super:"recurrence"`
}

// Dir picks the data folder: the flag, then $PLIM_DIR, then
// $XDG_DATA_HOME/plim, then ~/.local/share/plim. Never ~/.config, which
// is often kept in a dotfiles repo, and never the current directory.
func Dir(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if d := os.Getenv("PLIM_DIR"); d != "" {
		return d, nil
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "plim"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "plim"), nil
}

// Load reads every account and its items. A missing folder or file is empty.
func Load(dir string) ([]plan.Account, error) {
	var accountRecs []accountRecord
	if err := readTable(filepath.Join(dir, accountsFile), &accountRecs); err != nil {
		return nil, err
	}
	var itemRecs []itemRecord
	if err := readTable(filepath.Join(dir, itemsFile), &itemRecs); err != nil {
		return nil, err
	}

	var accounts []plan.Account
	index := map[string]int{}
	for _, r := range accountRecs {
		if _, dup := index[r.Name]; dup {
			return nil, fmt.Errorf("%s: duplicate account %q", accountsFile, r.Name)
		}
		asOf, err := parseDate(r.AsOf)
		if err != nil {
			return nil, fmt.Errorf("%s: account %q as_of: %w", accountsFile, r.Name, err)
		}
		index[r.Name] = len(accounts)
		accounts = append(accounts, plan.Account{Name: r.Name, Balance: plan.Cents(r.BalanceCents), AsOf: asOf})
	}

	ids := map[string]bool{}
	for _, r := range itemRecs {
		it, err := r.item()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", itemsFile, err)
		}
		if ids[it.ID] {
			return nil, fmt.Errorf("%s: duplicate id %q", itemsFile, it.ID)
		}
		ids[it.ID] = true
		i, ok := index[r.Account]
		if !ok {
			return nil, fmt.Errorf("%s: item %q: unknown account %q", itemsFile, it.ID, r.Account)
		}
		accounts[i].Items = append(accounts[i].Items, it)
	}
	return accounts, nil
}

func (r itemRecord) item() (plan.Item, error) {
	if r.ID == "" {
		return plan.Item{}, fmt.Errorf("item %q has no id", r.Description)
	}
	it := plan.Item{ID: r.ID, Description: r.Description, Amount: plan.Cents(r.AmountCents), Recurrence: r.Recurrence}
	var err error
	if it.Date, err = parseDate(r.Date); err != nil {
		return it, fmt.Errorf("item %q date: %w", r.ID, err)
	}
	if it.Origin, err = parseOptionalDate(r.Origin); err != nil {
		return it, fmt.Errorf("item %q origin: %w", r.ID, err)
	}
	if it.End, err = parseOptionalDate(r.End); err != nil {
		return it, fmt.Errorf("item %q end: %w", r.ID, err)
	}
	if _, err := recur.New(it.Date, it.Recurrence, recur.Options{}); err != nil {
		return it, fmt.Errorf("item %q: %w", r.ID, err)
	}
	return it, nil
}

// Save replaces both files. Each file is swapped in whole by rename, so a
// crash leaves the old or new version of each file, never a partial one.
func Save(dir string, accounts []plan.Account) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var accountRecs []any
	var itemRecs []any
	for _, a := range accounts {
		accountRecs = append(accountRecs, accountRecord{Name: a.Name, BalanceCents: int64(a.Balance), AsOf: formatDate(a.AsOf)})
		for _, it := range a.Items {
			itemRecs = append(itemRecs, itemRecord{
				ID:          it.ID,
				Account:     a.Name,
				Description: it.Description,
				AmountCents: int64(it.Amount),
				Date:        formatDate(it.Date),
				Origin:      formatOptionalDate(it.Origin),
				End:         formatOptionalDate(it.End),
				Recurrence:  it.Recurrence,
			})
		}
	}
	if err := writeTable(filepath.Join(dir, itemsFile), itemRecs); err != nil {
		return err
	}
	return writeTable(filepath.Join(dir, accountsFile), accountRecs)
}

func readTable[T any](path string, out *[]T) error {
	data, err := os.ReadFile(path) //nolint:gosec // path is in the data folder the user chose
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	r := supio.NewReader(super.NewContext(), bytes.NewReader(data))
	for {
		val, err := r.Read()
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		if val == nil {
			return nil
		}
		var rec T
		if err := super.Unmarshal(*val, &rec); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		*out = append(*out, rec)
	}
}

func writeTable(path string, recs []any) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	err = writeRecords(tmp, recs)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

func writeRecords(f *os.File, recs []any) error {
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	m := super.NewMarshaler(super.NewContext())
	w := supio.NewWriter(f, supio.WriterOpts{ColorDisabled: true})
	for _, rec := range recs {
		val, err := m.Marshal(rec)
		if err != nil {
			return err
		}
		if err := w.Write(val); err != nil {
			return err
		}
	}
	return f.Sync()
}

func parseDate(s string) (time.Time, error) { return time.Parse(time.DateOnly, s) }

func parseOptionalDate(s *string) (time.Time, error) {
	if s == nil {
		return time.Time{}, nil
	}
	return parseDate(*s)
}

func formatDate(t time.Time) string { return t.Format(time.DateOnly) }

func formatOptionalDate(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := formatDate(t)
	return &s
}
