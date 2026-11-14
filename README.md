# plimsoll

A ship's Plimsoll line marks how low it can safely sit in the water.
`plim` forecasts your bank balance from your recurring bills and income and
shows how low it will get before the next money arrives.

Your data lives in plain [SuperDB](https://superdb.org) `.sup` files in a
folder outside this repo: `--dir`, else `$PLIM_DIR`, else
`$XDG_DATA_HOME/plim` (`~/.local/share/plim`).

```sh
go install github.com/chrismo/plimsoll/cmd/plim@latest

plim open checking --balance 2500 --as-of 2026-11-10
plim add --desc Rent --amount -1000 --date 2026-12-01 --repeat monthly
plim add --desc Paycheck --amount 2000 --date 2026-11-20 --repeat "every 2 weeks"
plim ahead                  # daily balances and the low point
plim afford 400 --date 2026-11-25
plim pay rent@2026-12-01    # the date guards against paying twice
```

Every command takes `--json`. Run `plim` with no arguments for the full
list.

The data folder holds two tables, joined on the account name:

```
accounts.sup  {name, balance_cents, as_of}
items.sup     {id, account, description, amount_cents, date, origin, end, recurrence}
```

Status: early. The recurrence engine and forecast are ported from Cashew,
a Rails app from 2013.

## License

MIT
