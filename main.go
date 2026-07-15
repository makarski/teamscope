package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/makarski/teamscope/cmd"
	"github.com/makarski/teamscope/config"
)

func main() {
	configPath := flag.String("config", config.DefaultFileName, "Path to the teamscope config file")
	flag.Usage = usage
	flag.Parse()

	name := flag.Arg(0)
	if name == "" {
		usage()
		os.Exit(2)
	}

	if err := cmd.Run(context.Background(), name, *configPath, flag.Args()[1:]); err != nil {
		slog.Error("command failed", "command", name, "err", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(flag.CommandLine.Output(), `teamscope - team goal-alignment & work-mix observability

USAGE:
  teamscope [--config FILE] <command> [options]

COMMANDS:
  snapshot   Ingest Jira, classify work, score alignment, store a snapshot
             (posts to Slack when configured)
  serve      Render the dashboard
               --out FILE   write static HTML and exit
               --addr ADDR  serve over HTTP (default :8080)

OPTIONS:
`)
	flag.PrintDefaults()
}
