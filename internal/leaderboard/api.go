package leaderboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

const (
	// defaultTopN is used when the /top request omits n.
	defaultTopN = 10
	// maxTopN is the largest n accepted by /top; larger values are rejected.
	maxTopN = 1000
)

// API is the HTTP layer over a Board. Each API instance owns an isolated
// Board, so tests and the smoke check can spin up a fresh server per scenario
// without any shared global state.
type API struct {
	board *Board
}

// NewAPI returns an API backed by a fresh Board.
func NewAPI() *API { return &API{board: New()} }

// Board returns the underlying board. Exposed for direct unit testing.
func (a *API) Board() *Board { return a.board }

// Handler returns the HTTP mux serving all leaderboard endpoints.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /scores", a.handlePostScores)
	mux.HandleFunc("GET /scores/{player_id}", a.handleGetScore)
	mux.HandleFunc("DELETE /scores/{player_id}", a.handleDeleteScore)
	mux.HandleFunc("GET /top", a.handleTop)
	mux.HandleFunc("POST /reset", a.handleReset)
	mux.HandleFunc("GET /healthz", a.handleHealthz)
	return mux
}

type submitRequest struct {
	PlayerID string `json:"player_id"`
	Score    *int64 `json:"score"`
	Ts       *int64 `json:"ts"`
}

type submitResponse struct {
	PlayerID  string `json:"player_id"`
	BestScore int64  `json:"best_score"`
	BestTs    int64  `json:"best_ts"`
	Rank      int    `json:"rank"`
	Updated   bool   `json:"updated"`
	Created   bool   `json:"created"`
}

func (a *API) handlePostScores(w http.ResponseWriter, r *http.Request) {
	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Score == nil {
		writeError(w, http.StatusBadRequest, "score is required")
		return
	}
	if req.Ts == nil {
		writeError(w, http.StatusBadRequest, "ts is required")
		return
	}
	ent, updated, created, err := a.board.Submit(req.PlayerID, *req.Score, *req.Ts)
	if err != nil {
		if errors.Is(err, ErrInvalidPlayer) {
			writeError(w, http.StatusBadRequest, "player_id must be non-empty")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, submitResponse{
		PlayerID:  ent.PlayerID,
		BestScore: ent.BestScore,
		BestTs:    ent.BestTs,
		Rank:      ent.Rank,
		Updated:   updated,
		Created:   created,
	})
}

func (a *API) handleGetScore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("player_id")
	ent, err := a.board.Player(id)
	if err != nil {
		if errors.Is(err, ErrInvalidPlayer) {
			writeError(w, http.StatusBadRequest, "player_id must be non-empty")
			return
		}
		if errors.Is(err, ErrPlayerNotFound) {
			writeError(w, http.StatusNotFound, "player not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, ent)
}

type topResponse struct {
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"`
}

func (a *API) handleTop(w http.ResponseWriter, r *http.Request) {
	n := defaultTopN
	if raw := r.URL.Query().Get("n"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxTopN {
			writeError(w, http.StatusBadRequest, "n must be an integer in [1, 1000]")
			return
		}
		n = parsed
	}
	entries, total := a.board.TopN(n)
	writeJSON(w, http.StatusOK, topResponse{Entries: entries, Total: total})
}

type deleteResponse struct {
	PlayerID string `json:"player_id"`
	Deleted  bool   `json:"deleted"`
}

func (a *API) handleDeleteScore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("player_id")
	tid, ok := normalizeID(id)
	if !ok {
		writeError(w, http.StatusBadRequest, "player_id must be non-empty")
		return
	}
	if a.board.Remove(tid) {
		writeJSON(w, http.StatusOK, deleteResponse{PlayerID: tid, Deleted: true})
		return
	}
	writeError(w, http.StatusNotFound, "player not found")
}

type resetResponse struct {
	Reset   bool `json:"reset"`
	Cleared int  `json:"cleared"`
}

func (a *API) handleReset(w http.ResponseWriter, r *http.Request) {
	cleared := a.board.Reset()
	writeJSON(w, http.StatusOK, resetResponse{Reset: true, Cleared: cleared})
}

func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
