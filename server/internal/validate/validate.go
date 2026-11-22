// Package validate holds the input rules shared by the API and the import command.
package validate

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits, counted in Unicode code points.
const (
	MaxSideRunes     = 6000
	MaxDeckNameRunes = 100
)

// ErrInvalid is wrapped by every validation error.
var ErrInvalid = errors.New("invalid")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// NormalizeSide converts CRLF to LF, drops leading blank lines and trims
// trailing whitespace. The indentation of the first non-blank line is kept,
// because it may be code.
func NormalizeSide(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 || strings.TrimSpace(s[:i]) != "" {
			break
		}
		s = s[i+1:]
	}
	return strings.TrimRightFunc(s, unicode.IsSpace)
}

// Side normalises a card side and requires 1..MaxSideRunes code points.
func Side(field, s string) (string, error) {
	s = NormalizeSide(s)
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return "", invalid("%s must not be empty", field)
	}
	if n > MaxSideRunes {
		return "", invalid("%s is %d characters; the limit is %d", field, n, MaxSideRunes)
	}
	return s, nil
}

// DeckName trims a deck name and requires 1..MaxDeckNameRunes code points.
func DeckName(s string) (string, error) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return "", invalid("deck name must not be empty")
	}
	if n > MaxDeckNameRunes {
		return "", invalid("deck name is %d characters; the limit is %d", n, MaxDeckNameRunes)
	}
	return s, nil
}

// NewPerDay requires 0..9999.
func NewPerDay(n int) error {
	if n < 0 || n > 9999 {
		return invalid("newPerDay must be between 0 and 9999")
	}
	return nil
}

// DesiredRetention requires 0.70..0.99 inclusive.
func DesiredRetention(r float64) error {
	if !(r >= 0.70 && r <= 0.99) {
		return invalid("desiredRetention must be between 0.70 and 0.99")
	}
	return nil
}

// RolloverHour requires 0..23.
func RolloverHour(h int) error {
	if h < 0 || h > 23 {
		return invalid("dayRolloverHour must be between 0 and 23")
	}
	return nil
}
