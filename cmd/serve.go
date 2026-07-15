package cmd

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/makarski/teamscope/report"
)

// runServe renders the dashboard. With --out it writes a static HTML file and
// exits; otherwise it serves the dashboard over HTTP on --addr.
func runServe(ctx context.Context, configPath string, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	out := fs.String("out", "", "Write static HTML to this file and exit")
	addr := fs.String("addr", ":8080", "HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	d, err := newDeps(configPath)
	if err != nil {
		return err
	}
	defer d.close()

	renderer, err := report.NewWebRenderer(d.store)
	if err != nil {
		return err
	}

	if *out != "" {
		return writeStatic(ctx, renderer, *out)
	}
	return serveHTTP(renderer, *addr)
}

func writeStatic(ctx context.Context, renderer *report.WebRenderer, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("serve: create %s: %w", path, err)
	}
	defer f.Close()

	if err := renderer.Render(ctx, f); err != nil {
		return err
	}
	slog.Info("wrote dashboard", "path", path)
	return nil
}

func serveHTTP(renderer *report.WebRenderer, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := renderer.Render(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	slog.Info("serving dashboard", "addr", addr)
	return http.ListenAndServe(addr, mux)
}
