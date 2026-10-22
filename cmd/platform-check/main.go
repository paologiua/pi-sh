// platform-check is a local portability diagnostic, not a Connect CLI.
package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"pi-sh/internal/platform"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	r := platform.Check(ctx)
	if json.NewEncoder(os.Stdout).Encode(r) != nil {
		os.Exit(1)
	}
	for _, v := range r.Results {
		if !v.Passed {
			os.Exit(1)
		}
	}
}
