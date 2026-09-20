// Command prometheus-cli lets coding agents query Prometheus for a developer's
// investigation workflow: run PromQL instant and range queries, discover the
// metrics, labels and series behind them, and read scrape targets, rules and
// alerts — all with agent-friendly JSON output and structured errors.
package main

import (
	"os"

	"github.com/angelmsger/prometheus-cli/internal/app"
)

func main() {
	os.Exit(app.Execute())
}
