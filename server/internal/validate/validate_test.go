package validate

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeSideKeepsFirstLineIndent(t *testing.T) {
	got := NormalizeSide("\n\n    int& r = x;\r\nfoo  \n\n")
	if want := "    int& r = x;\nfoo"; got != want {
		t.Fatalf("NormalizeSide = %q, want %q", got, want)
	}
}

func TestSideLimits(t *testing.T) {
	if _, err := Side("front", strings.Repeat("→", MaxSideRunes)); err != nil {
		t.Fatalf("6000 runes: unexpected error %v", err)
	}
	if _, err := Side("front", strings.Repeat("→", MaxSideRunes+1)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("6001 runes: err = %v, want ErrInvalid", err)
	}
	for _, s := range []string{"", " \n\t"} {
		if _, err := Side("back", s); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Side(%q): err = %v, want ErrInvalid", s, err)
		}
	}
	_, err := Side("back", "")
	if !strings.Contains(err.Error(), "back") {
		t.Fatalf("error %q should name the field", err)
	}
}

func TestDeckName(t *testing.T) {
	got, err := DeckName("  Cpp  ")
	if err != nil || got != "Cpp" {
		t.Fatalf("DeckName = %q, %v", got, err)
	}
	if _, err := DeckName(strings.Repeat("é", MaxDeckNameRunes)); err != nil {
		t.Fatalf("100 runes: %v", err)
	}
	if _, err := DeckName(strings.Repeat("é", MaxDeckNameRunes+1)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("101 runes: err = %v", err)
	}
	if _, err := DeckName("   "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank: err = %v", err)
	}
}

func TestRanges(t *testing.T) {
	bad := []error{
		NewPerDay(-1), NewPerDay(10000),
		DesiredRetention(0.69), DesiredRetention(1.0),
		RolloverHour(-1), RolloverHour(24),
	}
	for i, err := range bad {
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("bad case %d: err = %v, want ErrInvalid", i, err)
		}
	}
	good := []error{
		NewPerDay(0), NewPerDay(9999),
		DesiredRetention(0.70), DesiredRetention(0.99),
		RolloverHour(0), RolloverHour(23),
	}
	for i, err := range good {
		if err != nil {
			t.Errorf("good case %d: unexpected %v", i, err)
		}
	}
}
