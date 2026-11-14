package plan

import (
	"fmt"
	"regexp"
	"strconv"
)

var centsRe = regexp.MustCompile(`^([+-]?)(\d*)(?:\.(\d{1,2}))?$`)

// ParseCents reads a plain decimal like "-1234.56". Like Cashew's API it
// rejects "$" and ",", and anything finer than cents.
func ParseCents(s string) (Cents, error) {
	m := centsRe.FindStringSubmatch(s)
	if m == nil || (m[2] == "" && m[3] == "") {
		return 0, fmt.Errorf("invalid amount %q: want a plain decimal like -1234.56", s)
	}
	frac := m[3]
	for len(frac) < 2 {
		frac += "0"
	}
	n, err := strconv.ParseInt(m[2]+frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: %w", s, err)
	}
	if m[1] == "-" {
		n = -n
	}
	return Cents(n), nil
}

func (c Cents) String() string {
	sign := ""
	n := int64(c)
	if n < 0 {
		sign, n = "-", -n
	}
	return fmt.Sprintf("%s%d.%02d", sign, n/100, n%100)
}
