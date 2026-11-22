package ratelimit

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)

func TestWindowAllowsLimitThenBlocks(t *testing.T) {
	w := &Window{Limit: 3, Period: time.Minute}
	for i := range 3 {
		if !w.Allow("k", t0.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("attempt %d blocked", i+1)
		}
	}
	if w.Allow("k", t0.Add(10*time.Second)) {
		t.Fatal("attempt 4 allowed")
	}
}

func TestWindowRolls(t *testing.T) {
	w := &Window{Limit: 1, Period: time.Minute}
	if !w.Allow("k", t0) {
		t.Fatal("first blocked")
	}
	if w.Allow("k", t0.Add(59*time.Second)) {
		t.Fatal("allowed inside the window")
	}
	if !w.Allow("k", t0.Add(time.Minute)) {
		t.Fatal("blocked after the window")
	}
}

func TestWindowKeysIndependent(t *testing.T) {
	w := &Window{Limit: 1, Period: time.Minute}
	if !w.Allow("a", t0) || !w.Allow("b", t0) {
		t.Fatal("keys share a budget")
	}
	if w.Allow("a", t0) {
		t.Fatal("a allowed twice")
	}
}

func TestWindowDropsIdleKeys(t *testing.T) {
	w := &Window{Limit: 1, Period: time.Minute}
	w.Allow("a", t0)
	w.Allow("b", t0.Add(2*time.Minute))
	if n := w.keys(); n != 1 {
		t.Fatalf("keys = %d, want 1 (idle key a dropped)", n)
	}
}
