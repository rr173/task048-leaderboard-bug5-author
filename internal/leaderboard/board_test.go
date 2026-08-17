package leaderboard

import (
	"errors"
	"testing"
)

func TestSubmitCreatesAndRanks(t *testing.T) {
	b := New()
	ent, updated, created, err := b.Submit("alice", 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !updated {
		t.Fatalf("first submit: created=%v updated=%v, want true true", created, updated)
	}
	if ent.BestScore != 100 || ent.BestTs != 1 || ent.Rank != 1 {
		t.Fatalf("ent=%+v, want score=100 ts=1 rank=1", ent)
	}
}

func TestPersonalBestDoesNotDecrease(t *testing.T) {
	b := New()
	b.Submit("alice", 100, 1)

	// Lower score: best unchanged, not updated.
	ent, updated, created, err := b.Submit("alice", 80, 2)
	if err != nil {
		t.Fatal(err)
	}
	if updated || created {
		t.Fatalf("lower score: updated=%v created=%v, want false false", updated, created)
	}
	if ent.BestScore != 100 || ent.BestTs != 1 {
		t.Fatalf("best changed on lower score: %+v", ent)
	}
	if ent.Rank != 1 {
		t.Fatalf("rank=%d want 1", ent.Rank)
	}

	// Equal score: best and reach time unchanged, not updated.
	ent, updated, _, _ = b.Submit("alice", 100, 3)
	if updated {
		t.Fatal("equal score should not update best")
	}
	if ent.BestTs != 1 {
		t.Fatalf("reach time overwritten on equal score: best_ts=%d want 1", ent.BestTs)
	}
}

func TestHigherScoreUpdatesReachTime(t *testing.T) {
	b := New()
	b.Submit("alice", 50, 1)
	ent, updated, _, _ := b.Submit("alice", 80, 5)
	if !updated {
		t.Fatal("higher score should update best")
	}
	if ent.BestScore != 80 || ent.BestTs != 5 {
		t.Fatalf("ent=%+v, want score=80 ts=5", ent)
	}
}

func TestSmallerEventTimeOnHigherScore(t *testing.T) {
	// A higher score submitted with an earlier event time stamps that earlier
	// time as the reach time (event-time semantics, not submission order).
	b := New()
	b.Submit("alice", 30, 10)
	ent, updated, _, _ := b.Submit("alice", 90, 3)
	if !updated {
		t.Fatal("higher score should update best")
	}
	if ent.BestScore != 90 || ent.BestTs != 3 {
		t.Fatalf("ent=%+v, want score=90 ts=3", ent)
	}
}

func TestCompetitionRanking(t *testing.T) {
	b := New()
	b.Submit("alice", 100, 1)
	b.Submit("bob", 100, 2)
	b.Submit("carol", 90, 3)

	for _, tc := range []struct {
		id   string
		rank int
	}{
		{"alice", 1}, {"bob", 1}, {"carol", 3},
	} {
		ent, err := b.Player(tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if ent.Rank != tc.rank {
			t.Errorf("%s rank=%d want %d", tc.id, ent.Rank, tc.rank)
		}
	}

	// Three-way tie plus a lower score: 100,100,100,90 -> 1,1,1,4.
	b.Submit("dave", 100, 4)
	if ent, _ := b.Player("dave"); ent.Rank != 1 {
		t.Errorf("dave rank=%d want 1", ent.Rank)
	}
	if ent, _ := b.Player("carol"); ent.Rank != 4 {
		t.Errorf("carol rank=%d want 4 (skipped after three-way tie)", ent.Rank)
	}
}

func TestTopNOrderTiebreaks(t *testing.T) {
	b := New()
	b.Submit("bob", 80, 5)
	b.Submit("alice", 80, 5) // same score, same reach time: id asc -> alice first
	b.Submit("carol", 80, 2) // same score, earlier reach time: carol first
	b.Submit("dave", 90, 9)  // higher score: dave first overall

	es, total := b.TopN(10)
	if total != 4 {
		t.Fatalf("total=%d want 4", total)
	}
	want := []string{"dave", "carol", "alice", "bob"}
	for i, e := range es {
		if e.PlayerID != want[i] {
			t.Fatalf("order[%d]=%s want %s (full=%v)", i, e.PlayerID, want[i], es)
		}
	}
	// dave rank 1; carol/alice/bob all 80 share rank 2.
	if es[0].Rank != 1 {
		t.Errorf("dave rank=%d want 1", es[0].Rank)
	}
	for _, e := range es[1:] {
		if e.Rank != 2 {
			t.Errorf("%s rank=%d want 2 (tie at 80)", e.PlayerID, e.Rank)
		}
	}
}

func TestTopNClampAndTotal(t *testing.T) {
	b := New()
	b.Submit("a", 5, 1)
	b.Submit("b", 4, 2)
	b.Submit("c", 3, 3)
	b.Submit("d", 2, 4)
	b.Submit("e", 1, 5)

	es, total := b.TopN(3)
	if total != 5 {
		t.Fatalf("total=%d want 5", total)
	}
	if len(es) != 3 {
		t.Fatalf("len=%d want 3", len(es))
	}
	// n larger than player count returns all.
	es, _ = b.TopN(100)
	if len(es) != 5 {
		t.Fatalf("len=%d want 5 (clamped to total)", len(es))
	}
}

func TestEmptyBoardTop(t *testing.T) {
	b := New()
	es, total := b.TopN(10)
	if total != 0 || len(es) != 0 {
		t.Fatalf("empty board: es=%v total=%d, want empty/0", es, total)
	}
}

func TestNegativeScores(t *testing.T) {
	b := New()
	b.Submit("alice", -50, 1)
	ent, updated, _, _ := b.Submit("alice", -10, 2)
	if !updated {
		t.Fatal("-10 should improve on -50")
	}
	if ent.BestScore != -10 {
		t.Errorf("best=%d want -10", ent.BestScore)
	}
	if ent.Rank != 1 {
		t.Errorf("rank=%d want 1", ent.Rank)
	}
}

func TestRemoveAndRecompute(t *testing.T) {
	b := New()
	b.Submit("alice", 100, 1)
	b.Submit("bob", 90, 2)
	b.Submit("carol", 90, 3)
	// bob and carol share rank 2 behind alice (rank 1).
	if ent, _ := b.Player("bob"); ent.Rank != 2 {
		t.Fatalf("bob rank=%d want 2", ent.Rank)
	}
	if !b.Remove("alice") {
		t.Fatal("remove alice should report found")
	}
	// After removal bob and carol share rank 1.
	if ent, _ := b.Player("bob"); ent.Rank != 1 {
		t.Fatalf("bob rank=%d want 1 after removal", ent.Rank)
	}
	if b.Remove("alice") {
		t.Fatal("remove alice again should report not found")
	}
	if _, err := b.Player("alice"); !errors.Is(err, ErrPlayerNotFound) {
		t.Errorf("player after remove: err=%v want ErrPlayerNotFound", err)
	}
}

func TestReset(t *testing.T) {
	b := New()
	b.Submit("a", 1, 1)
	b.Submit("b", 2, 2)
	if n := b.Reset(); n != 2 {
		t.Fatalf("reset cleared=%d want 2", n)
	}
	es, total := b.TopN(10)
	if total != 0 || len(es) != 0 {
		t.Fatalf("after reset: es=%v total=%d want empty/0", es, total)
	}
	// Reset on empty board is harmless.
	if n := b.Reset(); n != 0 {
		t.Fatalf("reset empty cleared=%d want 0", n)
	}
}

func TestTrimmedID(t *testing.T) {
	b := New()
	ent, _, _, err := b.Submit("  alice  ", 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ent.PlayerID != "alice" {
		t.Fatalf("id not trimmed: %q", ent.PlayerID)
	}
	// Lookup works with the trimmed id.
	if _, err := b.Player("alice"); err != nil {
		t.Fatalf("lookup trimmed id: %v", err)
	}
	// Lookup also trims the query.
	if _, err := b.Player("  alice  "); err != nil {
		t.Fatalf("lookup with untrimmed query: %v", err)
	}
}

func TestInvalidPlayer(t *testing.T) {
	b := New()
	if _, _, _, err := b.Submit("   ", 1, 1); !errors.Is(err, ErrInvalidPlayer) {
		t.Errorf("empty id: err=%v want ErrInvalidPlayer", err)
	}
	if _, err := b.Player("   "); !errors.Is(err, ErrInvalidPlayer) {
		t.Errorf("empty id lookup: err=%v want ErrInvalidPlayer", err)
	}
	if b.Remove("   ") {
		t.Error("remove empty id should return false")
	}
}

func TestDeleteAfterResetAllowsResubmit(t *testing.T) {
	b := New()
	b.Submit("alice", 100, 1)
	b.Reset()
	// After reset, alice is gone; resubmitting creates her fresh with reset
	// reach time.
	ent, _, created, err := b.Submit("alice", 50, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("resubmit after reset should create")
	}
	if ent.BestTs != 1 {
		t.Fatalf("reach time=%d want 1 (reset)", ent.BestTs)
	}
}
