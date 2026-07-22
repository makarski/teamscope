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

	renderer, err := report.NewWebRenderer(d.store, d.cfg.Jira.BaseURL)
	if err != nil {
		return err
	}
	trendRenderer, err := report.NewTrendRenderer(d.store, d.cfg.Jira.BaseURL)
	if err != nil {
		return err
	}

	if *out != "" {
		return writeStatic(ctx, renderer, trendRenderer, *out)
	}
	return serveHTTP(renderer, trendRenderer, *addr)
}

func writeStatic(ctx context.Context, renderer *report.WebRenderer, trendRenderer *report.TrendRenderer, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("serve: create %s: %w", path, err)
	}
	defer f.Close()

	if err := renderer.Render(ctx, f); err != nil {
		return err
	}
	slog.Info("wrote dashboard", "path", path)

	// Also write the trends page next to it.
	trendPath := path
	if i := len(path) - len(".html"); i > 0 && path[i:] == ".html" {
		trendPath = path[:i] + "-trends.html"
	} else {
		trendPath = path + "-trends"
	}
	tf, err := os.Create(trendPath)
	if err != nil {
		return fmt.Errorf("serve: create %s: %w", trendPath, err)
	}
	defer tf.Close()
	if err := trendRenderer.Render(ctx, tf); err != nil {
		return err
	}
	slog.Info("wrote trends page", "path", trendPath)
	return nil
}

func serveHTTP(renderer *report.WebRenderer, trendRenderer *report.TrendRenderer, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := renderer.Render(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/trends", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := trendRenderer.Render(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	slog.Info("serving dashboard", "addr", addr)
	return http.ListenAndServe(addr, mux)
}
