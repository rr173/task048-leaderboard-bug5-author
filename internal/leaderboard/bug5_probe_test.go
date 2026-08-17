package leaderboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbe_TrailingJSONIsRejected(t *testing.T) {
	srv := httptest.NewServer(NewAPI().Handler())
	defer srv.Close()

	body := `{"player_id":"alice","score":10,"ts":1} {"unexpected":true}`
	resp, err := http.Post(srv.URL+"/scores", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 for trailing JSON", resp.StatusCode)
	}
}
