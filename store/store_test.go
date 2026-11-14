package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chrismo/plimsoll/plan"
)

func date(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func sample() []plan.Account {
	return []plan.Account{
		{
			Name:    "checking",
			Balance: 123456,
			AsOf:    date("2026-03-02"),
			Items: []plan.Item{
				{ID: "rent", Description: "Rent", Amount: -150000, Date: date("2026-03-31"),
					Origin: date("2026-01-31"), Recurrence: "monthly"},
				{ID: "gym", Description: "Gym", Amount: -2500, Date: date("2026-03-05"),
					Origin: date("2026-03-05"), End: date("2026-12-31"), Recurrence: "every 2 weeks"},
			},
		},
		{Name: "savings", Balance: 500000, AsOf: date("2026-03-01")},
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDirPrecedence(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("PLIM_DIR", "")

	check := func(flag, want string) {
		t.Helper()
		got, err := Dir(flag)
		if err != nil || got != want {
			t.Errorf("Dir(%q) = %q, %v; want %q", flag, got, err, want)
		}
	}

	check("", "/home/x/.local/share/plim")
	t.Setenv("XDG_DATA_HOME", "/xdg")
	check("", "/xdg/plim")
	t.Setenv("PLIM_DIR", "/env/data")
	check("", "/env/data")
	check("/flag/data", "/flag/data")
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()

	if err := Save(dir, sample()); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(got, sample()) {
		t.Errorf("round trip mismatch\n got: %+v\nwant: %+v", got, sample())
	}
}

func TestSaveWritesOneTablePerFile(t *testing.T) {
	dir := t.TempDir()

	if err := Save(dir, sample()); err != nil {
		t.Fatal(err)
	}

	accounts, _ := os.ReadFile(filepath.Join(dir, "accounts.sup"))
	wantAccounts := `{name:"checking",balance_cents:123456,as_of:"2026-03-02"}
{name:"savings",balance_cents:500000,as_of:"2026-03-01"}
`
	if string(accounts) != wantAccounts {
		t.Errorf("accounts.sup\n got: %s\nwant: %s", accounts, wantAccounts)
	}

	items, _ := os.ReadFile(filepath.Join(dir, "items.sup"))
	wantItems := `{id:"rent",account:"checking",description:"Rent",amount_cents:-150000,date:"2026-03-31",origin:"2026-01-31",end:null,recurrence:"monthly"}
{id:"gym",account:"checking",description:"Gym",amount_cents:-2500,date:"2026-03-05",origin:"2026-03-05",end:"2026-12-31",recurrence:"every 2 weeks"}
`
	if string(items) != wantItems {
		t.Errorf("items.sup\n got: %s\nwant: %s", items, wantItems)
	}
}

func TestSaveKeepsDataPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plim")

	if err := Save(dir, sample()); err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]os.FileMode{
		dir:                                0o700,
		filepath.Join(dir, "accounts.sup"): 0o600,
		filepath.Join(dir, "items.sup"):    0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s: mode %o, want %o", path, got, want)
		}
	}
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()

	if err := Save(dir, sample()); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"accounts.sup", "items.sup"}; !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestLoadMissingDirIsEmpty(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "nope"))

	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v", got, err)
	}
}

// An agent editing by hand may reorder fields or leave optional ones out.
func TestLoadHandWrittenRecords(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "accounts.sup", `{as_of:"2026-03-02",name:"checking",balance_cents:100}`+"\n")
	write(t, dir, "items.sup", `{recurrence:"once",date:"2026-03-09",id:"x",account:"checking",description:"Dentist",amount_cents:-4000}`+"\n")

	got, err := Load(dir)

	if err != nil {
		t.Fatal(err)
	}
	want := []plan.Account{{Name: "checking", Balance: 100, AsOf: date("2026-03-02"), Items: []plan.Item{
		{ID: "x", Description: "Dentist", Amount: -4000, Date: date("2026-03-09"), Recurrence: "once"},
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestLoadRejectsBadData(t *testing.T) {
	const account = `{name:"checking",balance_cents:0,as_of:"2026-03-02"}` + "\n"
	item := func(fields string) string {
		return `{id:"a",account:"checking",description:"Thing",amount_cents:-1,date:"2026-03-02",recurrence:"monthly"` + fields + "}\n"
	}

	for name, tc := range map[string]struct{ accounts, items, wantErr string }{
		"unknown account":   {account, strings.Replace(item(""), `"checking"`, `"nope"`, 1), `"nope"`},
		"bad date":          {account, strings.Replace(item(""), "2026-03-02", "03/02/2026", 1), "03/02/2026"},
		"bad as_of":         {strings.Replace(account, "2026-03-02", "yesterday", 1), "", "yesterday"},
		"bad recurrence":    {account, strings.Replace(item(""), "monthly", "biweekly", 1), "biweekly"},
		"duplicate id":      {account, item("") + item(""), `"a"`},
		"duplicate account": {account + account, "", `"checking"`},
		"missing id":        {account, strings.Replace(item(""), `id:"a",`, "", 1), "id"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "accounts.sup", tc.accounts)
			write(t, dir, "items.sup", tc.items)

			_, err := Load(dir)

			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("want error mentioning %s, got %v", tc.wantErr, err)
			}
		})
	}
}
