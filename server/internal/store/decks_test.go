package store

import (
	"context"
	"errors"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestDeckCRUD(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d, err := s.CreateDeck(ctx, "Cpp", false, t0)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID <= 0 || d.NewPerDay != 20 || d.Name != "Cpp" || !d.CreatedAt.Equal(t0) {
		t.Fatalf("created %+v", d)
	}
	g, err := s.GetDeck(ctx, d.ID)
	if err != nil || g != d {
		t.Fatalf("GetDeck = %+v, %v; want %+v", g, err, d)
	}
	u, err := s.UpdateDeck(ctx, d.ID, DeckPatch{Name: ptr("C++")}, t0)
	if err != nil || u.Name != "C++" || u.NewPerDay != 20 {
		t.Fatalf("UpdateDeck name = %+v, %v", u, err)
	}
	u, err = s.UpdateDeck(ctx, d.ID, DeckPatch{NewPerDay: ptr(5)}, t0)
	if err != nil || u.Name != "C++" || u.NewPerDay != 5 {
		t.Fatalf("UpdateDeck newPerDay = %+v, %v", u, err)
	}
	list, err := s.ListDecks(ctx)
	if err != nil || len(list) != 1 || list[0] != u {
		t.Fatalf("ListDecks = %+v, %v", list, err)
	}
	if err := s.DeleteDeck(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDeck(ctx, d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDeck after delete: %v", err)
	}
	if err := s.DeleteDeck(ctx, d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteDeck twice: %v", err)
	}
	if _, err := s.UpdateDeck(ctx, d.ID, DeckPatch{Name: ptr("x")}, t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateDeck missing: %v", err)
	}
}

func TestDeckDuplicateName(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	if _, err := s.CreateDeck(ctx, "Cpp", false, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDeck(ctx, "Cpp", false, t0); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate create: %v", err)
	}
	other, err := s.CreateDeck(ctx, "Go", false, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateDeck(ctx, other.ID, DeckPatch{Name: ptr("Cpp")}, t0); !errors.Is(err, ErrConflict) {
		t.Fatalf("rename to existing: %v", err)
	}
}

func TestListDecksOrder(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	for _, n := range []string{"beta", "Alpha", "gamma"} {
		if _, err := s.CreateDeck(ctx, n, false, t0); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListDecks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Name != "Alpha" || list[1].Name != "beta" || list[2].Name != "gamma" {
		t.Fatalf("order = %v", list)
	}
}
