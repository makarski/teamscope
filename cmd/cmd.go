// Package cmd wires configuration into runnable teamscope subcommands.
package cmd

import (
	"context"
	"fmt"

	"github.com/makarski/teamscope/align"
	"github.com/makarski/teamscope/classify"
	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/ingest"
	"github.com/makarski/teamscope/pipeline"
	"github.com/makarski/teamscope/store"
)

// deps holds the collaborators shared across subcommands.
type deps struct {
	cfg   *config.Config
	store *store.Store
}

// newDeps loads config and opens the store.
func newDeps(configPath string) (*deps, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	st, err := store.Open(cfg.Store.Path)
	if err != nil {
		return nil, err
	}

	return &deps{cfg: cfg, store: st}, nil
}

func (d *deps) close() error {
	return d.store.Close()
}

// buildRunner assembles the ingest → classify → align → store pipeline.
func (d *deps) buildRunner() (*pipeline.Runner, error) {
	fetcher, err := ingest.NewJiraClient(d.cfg.Jira)
	if err != nil {
		return nil, err
	}

	return pipeline.NewRunner(
		fetcher,
		d.buildClassifier(),
		alignerOrNil(align.NewScorer(d.cfg.Anthropic, d.cfg.Goals)),
		d.store,
		d.cfg.Jira.StatusNames,
		d.cfg.GoalsHash(),
	), nil
}

// buildClassifier wires the rule engine with an optional AI fallback. A nil
// concrete AI classifier is passed as a nil interface so the orchestrator
// skips AI cleanly.
func (d *deps) buildClassifier() *classify.Classifier {
	rules := classify.NewRuleEngine(d.cfg.Classify)
	if ai := classify.NewAnthropicClassifier(d.cfg.Anthropic); ai != nil {
		return classify.NewClassifier(rules, ai)
	}
	return classify.NewClassifier(rules, nil)
}

func alignerOrNil(s *align.Scorer) pipeline.Aligner {
	if s == nil {
		return nil
	}
	return s
}

// Run dispatches a subcommand by name.
func Run(ctx context.Context, name, configPath string, args []string) error {
	switch name {
	case "snapshot":
		return runSnapshot(ctx, configPath)
	case "serve":
		return runServe(ctx, configPath, args)
	default:
		return fmt.Errorf("unknown command %q (want: snapshot | serve)", name)
	}
}
