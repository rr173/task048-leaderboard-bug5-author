package leaderboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestAPI(t *testing.T) (*API, string) {
	t.Helper()
	api := NewAPI()
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return api, srv.URL
}

func postScore(t *testing.T, base, id string, score, ts int64) (submitResponse, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"player_id": id, "score": score, "ts": ts})
	resp, err := http.Post(base+"/scores", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out submitResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out, resp.StatusCode
}

func TestHTTPSubmitAndGet(t *testing.T) {
	_, base := newTestAPI(t)
	out, status := postScore(t, base, "alice", 100, 1)
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200", status)
	}
	if !out.Created || !out.Updated {
		t.Fatalf("created=%v updated=%v want true true", out.Created, out.Updated)
	}
	if out.Rank != 1 || out.BestScore != 100 {
		t.Fatalf("out=%+v", out)
	}

	resp, err := http.Get(base + "/scores/alice")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d", resp.StatusCode)
	}
	var ent Entry
	if err := json.NewDecoder(resp.Body).Decode(&ent); err != nil {
		t.Fatal(err)
	}
	if ent.PlayerID != "alice" || ent.BestScore != 100 || ent.BestTs != 1 || ent.Rank != 1 {
		t.Fatalf("ent=%+v", ent)
	}
}

func TestHTTPCompetitionRanking(t *testing.T) {
	_, base := newTestAPI(t)
	postScore(t, base, "alice", 100, 1)
	postScore(t, base, "bob", 100, 2)
	postScore(t, base, "carol", 90, 3)

	resp, err := http.Get(base + "/top?n=10")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var top topResponse
	if err := json.NewDecoder(resp.Body).Decode(&top); err != nil {
		t.Fatal(err)
	}
	if top.Total != 3 {
		t.Fatalf("total=%d want 3", top.Total)
	}
	if len(top.Entries) != 3 {
		t.Fatalf("len=%d want 3", len(top.Entries))
	}
	order := []string{"alice", "bob", "carol"}
	ranks := []int{1, 1, 3}
	for i, e := range top.Entries {
		if e.PlayerID != order[i] {
			t.Fatalf("order[%d]=%s want %s", i, e.PlayerID, order[i])
		}
		if e.Rank != ranks[i] {
			t.Fatalf("%s rank=%d want %d", e.PlayerID, e.Rank, ranks[i])
		}
	}
}

func TestHTTPPersonalBestNotDecreasing(t *testing.T) {
	_, base := newTestAPI(t)
	postScore(t, base, "alice", 100, 1)
	out, _ := postScore(t, base, "alice", 80, 2)
	if out.Updated {
		t.Fatal("lower score should not update best")
	}
	out, _ = postScore(t, base, "alice", 100, 3)
	if out.Updated {
		t.Fatal("equal score should not update best")
	}
	if out.BestTs != 1 {
		t.Fatalf("best_ts=%d want 1 (reach time preserved)", out.BestTs)
	}
}

func TestHTTPTopNValidation(t *testing.T) {
	_, base := newTestAPI(t)
	for i, s := range []int64{5, 4, 3, 2, 1} {
		postScore(t, base, string(rune('a'+i)), s, int64(i+1))
	}
	for _, n := range []string{"0", "1001", "-1", "abc"} {
		resp, err := http.Get(base + "/top?n=" + n)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("n=%s status=%d want 400", n, resp.StatusCode)
		}
	}
	// Default n (omitted) returns up to 10; with 5 players all are returned.
	resp, err := http.Get(base + "/top")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var top topResponse
	if err := json.NewDecoder(resp.Body).Decode(&top); err != nil {
		t.Fatal(err)
	}
	if len(top.Entries) != 5 || top.Total != 5 {
		t.Fatalf("default top: len=%d total=%d want 5/5", len(top.Entries), top.Total)
	}
	// n larger than total returns all without error.
	resp2, err := http.Get(base + "/top?n=100")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("n=100 status=%d want 200", resp2.StatusCode)
	}
}

func TestHTTPSubmitValidation(t *testing.T) {
	_, base := newTestAPI(t)
	cases := []struct {
		name string
		body string
	}{
		{"empty player_id", `{"player_id":"  ","score":1,"ts":1}`},
		{"missing score", `{"player_id":"alice","ts":1}`},
		{"missing ts", `{"player_id":"alice","score":1}`},
		{"non-integer score", `{"player_id":"alice","score":1.5,"ts":1}`},
		{"bad json", `{not json`},
	}
	for _, tc := range cases {
		resp, err := http.Post(base+"/scores", "application/json", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status=%d want 400", tc.name, resp.StatusCode)
		}
	}
}

func TestHTTPDeleteAndReset(t *testing.T) {
	_, base := newTestAPI(t)
	postScore(t, base, "alice", 100, 1)
	postScore(t, base, "bob", 90, 2)

	req, _ := http.NewRequest(http.MethodDelete, base+"/scores/alice", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var del deleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&del); err != nil {
		t.Fatal(err)
	}
	if !del.Deleted || del.PlayerID != "alice" {
		t.Fatalf("delete=%+v", del)
	}

	// Delete again -> 404.
	req2, _ := http.NewRequest(http.MethodDelete, base+"/scores/alice", nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("re-delete status=%d want 404", resp2.StatusCode)
	}

	// Reset clears the remaining player.
	resp3, err := http.Post(base+"/reset", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	var rr resetResponse
	if err := json.NewDecoder(resp3.Body).Decode(&rr); err != nil {
		t.Fatal(err)
	}
	if !rr.Reset || rr.Cleared != 1 {
		t.Fatalf("reset=%+v want cleared=1", rr)
	}
}

func TestHTTPNotFound(t *testing.T) {
	_, base := newTestAPI(t)
	resp, err := http.Get(base + "/scores/ghost")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get missing status=%d want 404", resp.StatusCode)
	}
}

func TestHTTPHealthz(t *testing.T) {
	_, base := newTestAPI(t)
	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "ok" {
		t.Fatalf("healthz=%v want status=ok", got)
	}
}
