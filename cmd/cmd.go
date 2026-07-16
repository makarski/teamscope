// Package cmd wires configuration into runnable teamscope subcommands.
package cmd

import (
	"context"
	"fmt"

	"github.com/makarski/teamscope/align"
	"github.com/makarski/teamscope/classify"
	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/goals"
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

// buildRunner assembles the ingest → rubric → classify → align → store pipeline.
func (d *deps) buildRunner() (*pipeline.Runner, error) {
	fetcher, err := ingest.NewJiraClient(d.cfg.Jira)
	if err != nil {
		return nil, err
	}

	sources, err := d.buildSources(fetcher)
	if err != nil {
		return nil, err
	}

	return pipeline.NewRunner(pipeline.Deps{
		Fetcher:     fetcher,
		Sources:     sources,
		Factory:     d.buildFactory(),
		Aligner:     alignerOrNil(align.NewScorer(d.cfg.Anthropic, d.cfg.Bedrock)),
		Store:       d.store,
		StatusNames: d.cfg.Jira.StatusNames,
		GoalsHash:   d.cfg.GoalsHash(),
	}), nil
}

// buildSources resolves each team's rubric source from config.
func (d *deps) buildSources(fetcher *ingest.JiraClient) (map[string]pipeline.RubricSource, error) {
	adapter := labelAdapter{client: fetcher, statusNames: d.cfg.Jira.StatusNames}
	sources := make(map[string]pipeline.RubricSource, len(d.cfg.Teams))

	for _, team := range d.cfg.Teams {
		rubricCfg, ok := d.cfg.RubricByName(team.Rubric)
		if !ok {
			return nil, fmt.Errorf("cmd: team %q references unknown rubric %q", team.Name, team.Rubric)
		}
		src, err := buildSource(rubricCfg, adapter)
		if err != nil {
			return nil, fmt.Errorf("cmd: team %q: %w", team.Name, err)
		}
		sources[team.Name] = src
	}
	return sources, nil
}

func buildSource(r config.Rubric, adapter goals.LabelFetcher) (pipeline.RubricSource, error) {
	switch r.Source {
	case "static", "":
		return goals.NewStaticSource(staticRubric(r)), nil
	case "jira_label":
		if r.Label == "" || r.LabelProject == "" {
			return nil, fmt.Errorf("rubric %q: jira_label source needs label and label_project", r.Name)
		}
		return goals.NewJiraLabelSource(adapter, r.LabelProject, r.Label, r.Name, domain.Lens(r.Lens)), nil
	default:
		return nil, fmt.Errorf("rubric %q: unknown source %q (want: static | jira_label)", r.Name, r.Source)
	}
}

func staticRubric(r config.Rubric) domain.Rubric {
	criteria := make([]domain.Criterion, 0, len(r.Criteria))
	for _, c := range r.Criteria {
		lens := c.Lens
		if lens == "" {
			lens = r.Lens
		}
		criteria = append(criteria, domain.Criterion{
			Key:    c.Key,
			Title:  c.Title,
			Status: domain.Status(c.Status),
			Weight: c.Weight,
			Lens:   domain.Lens(lens),
		})
	}
	return domain.Rubric{Name: r.Name, Criteria: criteria}
}

// buildFactory wires the classifier factory with an optional AI fallback and
// the union of keyword hints across all rubrics.
func (d *deps) buildFactory() pipeline.ClassifierFactory {
	var ai classify.AIMapper
	if concrete := classify.NewAnthropicClassifier(d.cfg.Anthropic, d.cfg.Bedrock); concrete != nil {
		ai = concrete
	}
	return factoryAdapter{inner: classify.NewFactory(ai, d.keywordHints())}
}

// factoryAdapter adapts *classify.Factory to pipeline.ClassifierFactory by
// widening the concrete return type to the pipeline.Classifier interface.
type factoryAdapter struct {
	inner *classify.Factory
}

func (f factoryAdapter) For(rubric domain.Rubric) pipeline.Classifier {
	return f.inner.For(rubric)
}

// labelAdapter adapts the Jira client's FetchByLabel to goals.LabelFetcher,
// deriving each epic's done-state from the configured status buckets.
type labelAdapter struct {
	client      *ingest.JiraClient
	statusNames config.StatusNames
}

func (a labelAdapter) FetchByLabel(project, label string) ([]goals.LabelledEpic, error) {
	epics, err := a.client.FetchByLabel(project, label)
	if err != nil {
		return nil, err
	}
	out := make([]goals.LabelledEpic, 0, len(epics))
	for i := range epics {
		out = append(out, goals.LabelledEpic{
			Key:     epics[i].Epic.Key,
			Summary: epics[i].Epic.Fields.Summary,
			Done:    a.isDone(&epics[i]),
		})
	}
	return out, nil
}

func (a labelAdapter) isDone(re *ingest.RawEpic) bool {
	for _, s := range a.statusNames.Done {
		if re.EpicStatus() == s {
			return true
		}
	}
	return false
}

func (d *deps) keywordHints() []classify.KeywordHint {
	var hints []classify.KeywordHint
	for _, r := range d.cfg.Rubrics {
		for _, h := range r.KeywordHints {
			hints = append(hints, classify.KeywordHint{Keyword: h.Keyword, CriterionKey: h.Criterion})
		}
	}
	return hints
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
