package main

import (
	"testing"
)

func TestRender(t *testing.T) {
	img := render(192)
	if b := img.Bounds(); b.Dx() != 192 || b.Dy() != 192 {
		t.Fatalf("bounds = %v", b)
	}
	if got := img.RGBAAt(2, 2); got != desk {
		t.Errorf("corner = %v, want desk %v", got, desk)
	}
	// Centre of the front card, below the rule and between lines.
	if got := img.RGBAAt(58, 90); got != card {
		t.Errorf("front card = %v, want %v", got, card)
	}
	// The maskable safe zone: nothing but desk outside the central 80%.
	for _, p := range [][2]int{{17, 96}, {175, 96}, {96, 17}, {96, 175}} {
		if got := img.RGBAAt(p[0], p[1]); got != desk {
			t.Errorf("outside safe zone at %v = %v", p, got)
		}
	}
}
