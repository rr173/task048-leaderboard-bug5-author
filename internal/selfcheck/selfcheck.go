// Package selfcheck runs an end-to-end verification of the leaderboard
// service over HTTP. It is invoked by the --smoke-test flag and exits the
// process on completion.
//
// Each scenario builds a fresh API and serves it on a fresh httptest server,
// so no state leaks between checks.
package selfcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	"task048-leaderboard/internal/leaderboard"
)

// Run exercises the leaderboard across the scenarios in the specification,
// returning nil if every behavior matches.
func Run() error {
	scenarios := []struct {
		name string
		fn   func(string) error
	}{
		{"个人最佳与竞赛排名", scenarioPersonalBestAndRanking},
		{"个人最佳不下降", scenarioPersonalBestNotDecreasing},
		{"刷新最佳更新达到时间", scenarioRefreshUpdatesReachTime},
		{"事件时间更小的高分", scenarioSmallerEventTimeOnHigherScore},
		{"负分", scenarioNegativeScores},
		{"删除后重算", scenarioDeleteAndRecompute},
		{"重置", scenarioReset},
		{"TopN与total解耦", scenarioTopNAndTotal},
		{"参数校验", scenarioValidation},
		{"健康检查", scenarioHealthz},
	}
	for _, sc := range scenarios {
		api := leaderboard.NewAPI()
		srv := httptest.NewServer(api.Handler())
		err := sc.fn(srv.URL)
		srv.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", sc.name, err)
		}
	}
	return nil
}

// --- typed response structs; bool fields decode to bool, never float64 ---

type submitResp struct {
	PlayerID  string `json:"player_id"`
	BestScore int64  `json:"best_score"`
	BestTs    int64  `json:"best_ts"`
	Rank      int    `json:"rank"`
	Updated   bool   `json:"updated"`
	Created   bool   `json:"created"`
}

type playerResp struct {
	PlayerID  string `json:"player_id"`
	BestScore int64  `json:"best_score"`
	BestTs    int64  `json:"best_ts"`
	Rank      int    `json:"rank"`
}

type entryResp struct {
	PlayerID  string `json:"player_id"`
	BestScore int64  `json:"best_score"`
	BestTs    int64  `json:"best_ts"`
	Rank      int    `json:"rank"`
}

type topResp struct {
	Entries []entryResp `json:"entries"`
	Total   int         `json:"total"`
}

type deleteResp struct {
	PlayerID string `json:"player_id"`
	Deleted  bool   `json:"deleted"`
}

type resetResp struct {
	Reset   bool `json:"reset"`
	Cleared int  `json:"cleared"`
}

type errResp struct {
	Error string `json:"error"`
}

// --- HTTP helpers ---

var client = &http.Client{}

func doRequest(method, url string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

func postScore(base, id string, score, ts int64) (submitResp, int, error) {
	status, data, err := doRequest("POST", base+"/scores",
		map[string]any{"player_id": id, "score": score, "ts": ts})
	if err != nil {
		return submitResp{}, status, err
	}
	var r submitResp
	if status == http.StatusOK {
		if err := json.Unmarshal(data, &r); err != nil {
			return r, status, fmt.Errorf("decode submit: %w (body=%s)", err, data)
		}
	}
	return r, status, nil
}

func getScore(base, id string) (playerResp, int, error) {
	status, data, err := doRequest("GET", base+"/scores/"+id, nil)
	if err != nil {
		return playerResp{}, status, err
	}
	var r playerResp
	if status == http.StatusOK {
		if err := json.Unmarshal(data, &r); err != nil {
			return r, status, fmt.Errorf("decode player: %w (body=%s)", err, data)
		}
	}
	return r, status, nil
}

func getTop(base string, n int) (topResp, int, error) {
	url := base + "/top"
	if n >= 0 {
		url += "?n=" + strconv.Itoa(n)
	}
	status, data, err := doRequest("GET", url, nil)
	if err != nil {
		return topResp{}, status, err
	}
	var r topResp
	if status == http.StatusOK {
		if err := json.Unmarshal(data, &r); err != nil {
			return r, status, fmt.Errorf("decode top: %w (body=%s)", err, data)
		}
	}
	return r, status, nil
}

func deleteScore(base, id string) (deleteResp, int, error) {
	status, data, err := doRequest("DELETE", base+"/scores/"+id, nil)
	if err != nil {
		return deleteResp{}, status, err
	}
	var r deleteResp
	if status == http.StatusOK {
		if err := json.Unmarshal(data, &r); err != nil {
			return r, status, fmt.Errorf("decode delete: %w (body=%s)", err, data)
		}
	}
	return r, status, nil
}

func resetBoard(base string) (resetResp, int, error) {
	status, data, err := doRequest("POST", base+"/reset", nil)
	if err != nil {
		return resetResp{}, status, err
	}
	var r resetResp
	if err := json.Unmarshal(data, &r); err != nil {
		return r, status, fmt.Errorf("decode reset: %w (body=%s)", err, data)
	}
	return r, status, nil
}

// --- helpers for assertions ---

func entries(top topResp) []string {
	out := make([]string, 0, len(top.Entries))
	for _, e := range top.Entries {
		out = append(out, e.PlayerID)
	}
	return out
}

func ranks(top topResp) []int {
	out := make([]int, 0, len(top.Entries))
	for _, e := range top.Entries {
		out = append(out, e.Rank)
	}
	return out
}

func eqStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func eqIntSlices(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// --- scenarios ---

func scenarioPersonalBestAndRanking(base string) error {
	postScore(base, "alice", 100, 1)
	postScore(base, "bob", 100, 2)
	postScore(base, "carol", 90, 3)

	// alice and bob tie for rank 1; carol is rank 3.
	top, status, err := getTop(base, 10)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("top status=%d", status)
	}
	if top.Total != 3 {
		return fmt.Errorf("total=%d want 3", top.Total)
	}
	if !eqStringSlices(entries(top), []string{"alice", "bob", "carol"}) {
		return fmt.Errorf("order=%v want [alice bob carol] (alice before bob: smaller best_ts)", entries(top))
	}
	if !eqIntSlices(ranks(top), []int{1, 1, 3}) {
		return fmt.Errorf("ranks=%v want [1 1 3] (competition ranking)", ranks(top))
	}
	return nil
}

func scenarioPersonalBestNotDecreasing(base string) error {
	postScore(base, "alice", 100, 1)

	r, status, err := postScore(base, "alice", 80, 2)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("lower score status=%d", status)
	}
	if r.Updated {
		return fmt.Errorf("lower score updated=true, want false (best not decreasing)")
	}
	if r.BestScore != 100 {
		return fmt.Errorf("lower score best=%d want 100", r.BestScore)
	}

	r, _, _ = postScore(base, "alice", 100, 3)
	if r.Updated {
		return fmt.Errorf("equal score updated=true, want false")
	}

	// Reach time stays 1, not overwritten by ts=2 or ts=3.
	p, _, err := getScore(base, "alice")
	if err != nil {
		return err
	}
	if p.BestTs != 1 {
		return fmt.Errorf("best_ts=%d want 1 (reach time preserved)", p.BestTs)
	}
	if p.BestScore != 100 {
		return fmt.Errorf("best=%d want 100", p.BestScore)
	}
	return nil
}

func scenarioRefreshUpdatesReachTime(base string) error {
	postScore(base, "alice", 50, 1)
	r, _, err := postScore(base, "alice", 80, 5)
	if err != nil {
		return err
	}
	if !r.Updated {
		return fmt.Errorf("80 should refresh best from 50")
	}
	if r.BestTs != 5 {
		return fmt.Errorf("best_ts=%d want 5", r.BestTs)
	}
	// bob reaches 80 earlier; he should sort before alice (same score, smaller best_ts).
	postScore(base, "bob", 80, 2)

	top, _, err := getTop(base, 10)
	if err != nil {
		return err
	}
	if !eqStringSlices(entries(top), []string{"bob", "alice"}) {
		return fmt.Errorf("order=%v want [bob alice] (bob reached 80 earlier)", entries(top))
	}
	// Both share rank 2? No: 80 is the top score, so both are rank 1.
	if !eqIntSlices(ranks(top), []int{1, 1}) {
		return fmt.Errorf("ranks=%v want [1 1] (both at top score 80)", ranks(top))
	}
	return nil
}

func scenarioSmallerEventTimeOnHigherScore(base string) error {
	postScore(base, "alice", 30, 10)
	r, _, err := postScore(base, "alice", 90, 3)
	if err != nil {
		return err
	}
	if !r.Updated {
		return fmt.Errorf("90 should refresh best from 30")
	}
	if r.BestScore != 90 {
		return fmt.Errorf("best=%d want 90", r.BestScore)
	}
	if r.BestTs != 3 {
		return fmt.Errorf("best_ts=%d want 3 (event-time semantics: smaller ts of higher score)", r.BestTs)
	}
	return nil
}

func scenarioNegativeScores(base string) error {
	postScore(base, "alice", -50, 1)
	r, _, err := postScore(base, "alice", -10, 2)
	if err != nil {
		return err
	}
	if !r.Updated {
		return fmt.Errorf("-10 should improve on -50")
	}
	if r.BestScore != -10 {
		return fmt.Errorf("best=%d want -10", r.BestScore)
	}
	if r.Rank != 1 {
		return fmt.Errorf("rank=%d want 1", r.Rank)
	}
	return nil
}

func scenarioDeleteAndRecompute(base string) error {
	postScore(base, "alice", 100, 1)
	postScore(base, "bob", 90, 2)
	postScore(base, "carol", 90, 3)

	// bob and carol tie at rank 2 behind alice (rank 1).
	p, _, err := getScore(base, "bob")
	if err != nil {
		return err
	}
	if p.Rank != 2 {
		return fmt.Errorf("bob rank=%d want 2 (before delete)", p.Rank)
	}

	d, status, err := deleteScore(base, "alice")
	if err != nil {
		return err
	}
	if status != http.StatusOK || !d.Deleted {
		return fmt.Errorf("delete alice: status=%d deleted=%v", status, d.Deleted)
	}

	// After removal bob and carol share rank 1.
	p, _, err = getScore(base, "bob")
	if err != nil {
		return err
	}
	if p.Rank != 1 {
		return fmt.Errorf("bob rank=%d want 1 (after delete alice)", p.Rank)
	}

	top, _, err := getTop(base, 10)
	if err != nil {
		return err
	}
	if top.Total != 2 {
		return fmt.Errorf("total=%d want 2 after delete", top.Total)
	}

	// Re-delete alice -> 404.
	_, status, _ = deleteScore(base, "alice")
	if status != http.StatusNotFound {
		return fmt.Errorf("re-delete alice status=%d want 404", status)
	}
	return nil
}

func scenarioReset(base string) error {
	postScore(base, "alice", 100, 1)
	postScore(base, "bob", 90, 2)

	rr, status, err := resetBoard(base)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("reset status=%d", status)
	}
	if !rr.Reset {
		return fmt.Errorf("reset=false want true")
	}
	if rr.Cleared != 2 {
		return fmt.Errorf("cleared=%d want 2", rr.Cleared)
	}

	top, _, err := getTop(base, 10)
	if err != nil {
		return err
	}
	if top.Total != 0 || len(top.Entries) != 0 {
		return fmt.Errorf("after reset: entries=%v total=%d want empty/0", entries(top), top.Total)
	}
	return nil
}

func scenarioTopNAndTotal(base string) error {
	for i, s := range []int64{5, 4, 3, 2, 1} {
		postScore(base, "p"+strconv.Itoa(i), s, int64(i+1))
	}

	// n=3 returns 3 entries but total=5.
	top, status, err := getTop(base, 3)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("n=3 status=%d", status)
	}
	if len(top.Entries) != 3 {
		return fmt.Errorf("n=3 len=%d want 3", len(top.Entries))
	}
	if top.Total != 5 {
		return fmt.Errorf("n=3 total=%d want 5", top.Total)
	}

	// n=100 returns all 5 without error.
	top, status, err = getTop(base, 100)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("n=100 status=%d", status)
	}
	if len(top.Entries) != 5 {
		return fmt.Errorf("n=100 len=%d want 5 (clamped to total)", len(top.Entries))
	}

	// n=0 and n=1001 are rejected.
	for _, n := range []int{0, 1001} {
		_, status, err := getTop(base, n)
		if err != nil {
			return err
		}
		if status != http.StatusBadRequest {
			return fmt.Errorf("n=%d status=%d want 400", n, status)
		}
	}

	// Default n (omitted) returns up to 10; 5 players -> 5 entries.
	_, status, err = getTop(base, -1) // -1 means omit the param
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("default top status=%d", status)
	}
	return nil
}

func scenarioValidation(base string) error {
	// Valid-JSON-but-invalid-field cases go through the normal marshal path.
	jsonCases := []struct {
		name string
		body map[string]any
	}{
		{"empty player_id", map[string]any{"player_id": "  ", "score": 1, "ts": 1}},
		{"missing score", map[string]any{"player_id": "alice", "ts": 1}},
		{"missing ts", map[string]any{"player_id": "alice", "score": 1}},
	}
	for _, tc := range jsonCases {
		status, _, err := doRequest("POST", base+"/scores", tc.body)
		if err != nil {
			return err
		}
		if status != http.StatusBadRequest {
			return fmt.Errorf("%s: status=%d want 400", tc.name, status)
		}
	}
	// Malformed JSON is sent verbatim so the server, not the client, rejects it.
	status, data, err := doRawRequest("POST", base+"/scores", "application/json", "not json")
	if err != nil {
		return err
	}
	if status != http.StatusBadRequest {
		return fmt.Errorf("bad json: status=%d want 400", status)
	}
	// Verify the error body is JSON with an "error" field.
	status, data, err = doRawRequest("POST", base+"/scores", "application/json",
		`{"player_id":"  ","score":1,"ts":1}`)
	if err != nil {
		return err
	}
	if status != http.StatusBadRequest {
		return fmt.Errorf("empty id status=%d want 400", status)
	}
	var e errResp
	if err := json.Unmarshal(data, &e); err != nil {
		return fmt.Errorf("decode error body: %w (body=%s)", err, data)
	}
	if strings.TrimSpace(e.Error) == "" {
		return fmt.Errorf("error body empty: %s", data)
	}
	return nil
}

func scenarioHealthz(base string) error {
	status, data, err := doRequest("GET", base+"/healthz", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("healthz status=%d", status)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		return fmt.Errorf("decode healthz: %w (body=%s)", err, data)
	}
	if got["status"] != "ok" {
		return fmt.Errorf("healthz=%v want status=ok", got)
	}
	return nil
}

// doRawRequest sends an arbitrary string body verbatim, bypassing JSON
// marshalling so malformed payloads reach the server unchanged.
func doRawRequest(method, url, contentType, body string) (int, []byte, error) {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}
