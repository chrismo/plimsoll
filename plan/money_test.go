package plan

import "testing"

func TestParseCents(t *testing.T) {
	for in, want := range map[string]Cents{
		"0":        0,
		"25":       2500,
		"25.5":     2550,
		"25.50":    2550,
		"-1234.56": -123456,
		"+3.07":    307,
		"0.01":     1,
		"-0.99":    -99,
		".75":      75,
	} {
		got, err := ParseCents(in)
		if err != nil || got != want {
			t.Errorf("ParseCents(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}

func TestParseCentsRejects(t *testing.T) {
	for _, in := range []string{"", "-", "$25", "1,234.56", "1.234", "abc", "1.2.3", "1e3", " 5", "92233720368547758.08"} {
		if got, err := ParseCents(in); err == nil {
			t.Errorf("ParseCents(%q) = %d, want error", in, got)
		}
	}
}

func TestCentsString(t *testing.T) {
	for in, want := range map[Cents]string{
		0:       "0.00",
		5:       "0.05",
		-5:      "-0.05",
		2550:    "25.50",
		-123456: "-1234.56",
	} {
		if got := in.String(); got != want {
			t.Errorf("Cents(%d).String() = %q, want %q", in, got, want)
		}
	}
}
