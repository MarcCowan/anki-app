package store

import (
	"context"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	got, err := s.GetSettings(ctx)
	if err != nil || got != (Settings{DesiredRetention: 0.9, DayRolloverHour: 4}) {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	want := Settings{DesiredRetention: 0.85, DayRolloverHour: 3}
	if err := s.UpdateSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetSettings(ctx)
	if err != nil || got != want {
		t.Fatalf("after update = %+v, %v", got, err)
	}
}
