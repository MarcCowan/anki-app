// Package clock provides an injectable source of the current time.
package clock

import "time"

// Clock returns the current time.
type Clock interface{ Now() time.Time }

// Real is the system clock.
type Real struct{}

// Now returns time.Now().
func (Real) Now() time.Time { return time.Now() }

// Fake is a manually controlled clock for tests.
type Fake struct{ T time.Time }

// Now returns the fake's current time.
func (f *Fake) Now() time.Time { return f.T }

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) { f.T = f.T.Add(d) }
