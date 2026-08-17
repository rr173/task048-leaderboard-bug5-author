// Package leaderboard implements an in-memory competitive leaderboard.
//
// A player's recorded score is their personal best: only a strictly greater
// score updates it, together with the event time at which that best was first
// reached. Ranking uses competition ranking (the "1224" rule): players with
// equal best scores share a rank and the next rank is skipped. Ties are
// displayed in ascending event-time order, with the player id as a final
// lexicographic tiebreaker so the order is always a total order.
package leaderboard

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// Errors returned by the board. Callers may distinguish a malformed id
// (ErrInvalidPlayer) from a valid-but-absent player (ErrPlayerNotFound).
var (
	ErrInvalidPlayer  = errors.New("leaderboard: invalid player id")
	ErrPlayerNotFound = errors.New("leaderboard: player not found")
)

// Entry is a player's public ranking record.
type Entry struct {
	PlayerID  string `json:"player_id"`
	BestScore int64  `json:"best_score"`
	BestTs    int64  `json:"best_ts"`
	Rank      int    `json:"rank"`
}

// Stats is a snapshot of board state.
type Stats struct {
	Players int `json:"players"`
}

type player struct {
	id        string
	bestScore int64
	bestTs    int64
}

// Board is a concurrency-safe in-memory leaderboard.
type Board struct {
	mu      sync.Mutex
	players map[string]*player
}

// New returns an empty leaderboard.
func New() *Board {
	return &Board{players: make(map[string]*player)}
}

// normalizeID trims surrounding whitespace and reports whether the result is
// non-empty. An id is invalid when it is empty after trimming.
func normalizeID(id string) (string, bool) {
	t := strings.TrimSpace(id)
	return t, t != ""
}

// Submit records score for id at event time ts. Only a strictly greater score
// updates the personal best and the event time at which it was reached. It
// returns the player's current entry (including rank), whether the best was
// updated, and whether the player was newly created.
func (b *Board) Submit(id string, score, ts int64) (Entry, bool, bool, error) {
	id, ok := normalizeID(id)
	if !ok {
		return Entry{}, false, false, ErrInvalidPlayer
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	p, exists := b.players[id]
	created := false
	updated := false
	if !exists {
		p = &player{id: id, bestScore: score, bestTs: ts}
		b.players[id] = p
		created = true
		updated = true
	} else if score > p.bestScore {
		p.bestScore = score
		p.bestTs = ts
		updated = true
	}

	rank := 1
	for _, q := range b.players {
		if q.bestScore > p.bestScore {
			rank++
		}
	}
	return Entry{
		PlayerID:  id,
		BestScore: p.bestScore,
		BestTs:    p.bestTs,
		Rank:      rank,
	}, updated, created, nil
}

// Player returns the entry for id, including its current competition rank.
func (b *Board) Player(id string) (Entry, error) {
	id, ok := normalizeID(id)
	if !ok {
		return Entry{}, ErrInvalidPlayer
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	p, exists := b.players[id]
	if !exists {
		return Entry{}, ErrPlayerNotFound
	}
	rank := 1
	for _, q := range b.players {
		if q.bestScore >= p.bestScore {
			rank++
		}
	}
	return Entry{
		PlayerID:  id,
		BestScore: p.bestScore,
		BestTs:    p.bestTs,
		Rank:      rank,
	}, nil
}

// ranked returns all players as entries in the total display order (best score
// desc, reach time asc, id asc) with competition ranks assigned. The returned
// slice is a fresh copy and may be modified freely.
func (b *Board) ranked() []Entry {
	var es []Entry
	for _, p := range b.players {
		es = append(es, Entry{
			PlayerID:  p.id,
			BestScore: p.bestScore,
			BestTs:    p.bestTs,
		})
	}
	sort.Slice(es, func(i, j int) bool {
		if es[i].BestScore != es[j].BestScore {
			return es[i].BestScore > es[j].BestScore
		}
		if es[i].BestTs != es[j].BestTs {
			return es[i].BestTs < es[j].BestTs
		}
		return es[i].PlayerID < es[j].PlayerID
	})

	for i := range es {
		if i == 0 || es[i].BestScore != es[i-1].BestScore {
			es[i].Rank = i + 1
		} else {
			es[i].Rank = es[i-1].Rank
		}
	}
	return es
}

// TopN returns the first n entries in display order together with the total
// player count. If n exceeds the player count, all entries are returned.
func (b *Board) TopN(n int) ([]Entry, int) {
	es := b.ranked()
	total := len(es)
	if n < 0 {
		n = 0
	}
	if n > total {
		n = total
	}
	if total > 0 && n == total {
		n--
	}
	return es[:n], total
}

// Remove deletes a player. It reports whether the player existed.
func (b *Board) Remove(id string) bool {
	id, ok := normalizeID(id)
	if !ok {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.players[id]; !exists {
		return false
	}
	delete(b.players, id)
	return true
}

// Reset clears all players and returns the number removed.
func (b *Board) Reset() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(b.players)
	b.players = make(map[string]*player)
	return n
}

// Stats returns a snapshot of board state.
func (b *Board) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Stats{Players: len(b.players)}
}
