// Command task048-leaderboard runs the in-memory competitive leaderboard
// service.
//
// Use --smoke-test to run the built-in self-check, which exits the process on
// completion. Otherwise the service serves HTTP on the address given by -addr.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"task048-leaderboard/internal/leaderboard"
	"task048-leaderboard/internal/selfcheck"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	smoke := flag.Bool("smoke-test", false, "run the built-in self-check and exit")
	flag.Parse()

	if *smoke {
		if err := selfcheck.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "smoke-test FAILED: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("smoke-test PASSED")
		return
	}

	api := leaderboard.NewAPI()
	srv := &http.Server{Addr: *addr, Handler: api.Handler()}
	log.Printf("leaderboard listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
