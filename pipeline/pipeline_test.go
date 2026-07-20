package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andygrunwald/go-jira"

	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/ingest"
)

func rawEpic(key, summary, status string) ingest.RawEpic {
	return ingest.RawEpic{
		Epic: jira.Issue{
			Key:    key,
			Fields: &jira.IssueFields{Summary: summary, Status: &jira.Status{Name: status}},
		},
		Issues: []jira.Issue{
			{Fields: &jira.IssueFields{Status: &jira.Status{Name: status}}},
		},
	}
}

var testRubric = domain.Rubric{Name: "work", Criteria: []domain.Criterion{
	{Key: "business", Title: "Business", Lens: domain.LensBusiness},
}}

type stubFetcher struct {
	epics      []ingest.RawEpic
	standalone []ingest.RawEpic
	err        error
}

func (s stubFetcher) FetchEpics(string) ([]ingest.RawEpic, error) { return s.epics, s.err }

func (s stubFetcher) FetchStandaloneIssues(string, map[string]bool) ([]ingest.RawEpic, error) {
	return s.standalone, s.err
}

type stubSource struct{ err error }

func (s stubSource) Rubric(context.Context, string) (domain.Rubric, error) {
	return testRubric, s.err
}

type stubClassifier struct{}

func (stubClassifier) Classify(context.Context, *ingest.RawEpic) domain.CriterionRef {
	return domain.CriterionRef{Key: "business", Source: domain.SourceLabel}
}

type stubFactory struct{}

func (stubFactory) For(domain.Rubric) Classifier { return stubClassifier{} }

type stubAligner struct {
	err error
}

func (s stubAligner) Score(context.Context, *ingest.RawEpic, domain.Criterion) (domain.Advancement, string, error) {
	if s.err != nil {
		return domain.AdvUnscored, "", s.err
	}
	return domain.AdvAdvances, "on target", nil
}

type stubStore struct {
	saved domain.Snapshot
}

func (s *stubStore) Save(_ context.Context, snap domain.Snapshot) (int64, error) {
	s.saved = snap
	return 42, nil
}

var statusNames = config.StatusNames{Done: []string{"Done"}, ToDo: []string{"To Do"}}

func newRunner(f Fetcher, src RubricSource, a Aligner, store Store) *Runner {
	sources := map[string]RubricSource{"Payments": src, "P": src}
	r := NewRunner(Deps{
		Fetcher:     f,
		Sources:     sources,
		Factory:     stubFactory{},
		Aligner:     a,
		Store:       store,
		StatusNames: statusNames,
		GoalsHash:   "hash1",
	})
	r.now = func() time.Time { return time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC) }
	return r
}

func TestRunEnrichesAndSaves(t *testing.T) {
	fetcher := stubFetcher{epics: []ingest.RawEpic{rawEpic("PT-1", "Billing", "Done")}}
	store := &stubStore{}
	runner := newRunner(fetcher, stubSource{}, stubAligner{}, store)

	id, err := runner.Run(context.Background(), config.Team{Name: "Payments", JiraProjects: []string{"PT"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}

	assertSnapshotMeta(t, store.saved)
	assertEpicEnrichment(t, store.saved)
}

func assertSnapshotMeta(t *testing.T, snap domain.Snapshot) {
	t.Helper()
	if snap.Team != "Payments" {
		t.Errorf("team = %q, want Payments", snap.Team)
	}
	if snap.GoalsHash != "hash1" {
		t.Errorf("goals hash = %q, want hash1", snap.GoalsHash)
	}
	if snap.Rubric.Name != "work" {
		t.Errorf("rubric = %q, want work", snap.Rubric.Name)
	}
}

func assertEpicEnrichment(t *testing.T, snap domain.Snapshot) {
	t.Helper()
	if len(snap.Epics) != 1 {
		t.Fatalf("want 1 epic, got %d", len(snap.Epics))
	}
	e := snap.Epics[0]
	if e.Key != "PT-1" {
		t.Errorf("key = %q, want PT-1", e.Key)
	}
	if e.Criterion.Key != "business" || e.Criterion.Advances != domain.AdvAdvances {
		t.Errorf("criterion wrong: %+v", e.Criterion)
	}
	if e.Lens != domain.LensBusiness {
		t.Errorf("lens = %q, want business", e.Lens)
	}
	if e.Status != domain.StatusDone {
		t.Errorf("status = %q, want done", e.Status)
	}
}

func TestRunNilAlignerLeavesAdvancesUnscored(t *testing.T) {
	fetcher := stubFetcher{epics: []ingest.RawEpic{rawEpic("PT-1", "Billing", "Done")}}
	store := &stubStore{}
	runner := newRunner(fetcher, stubSource{}, nil, store)

	if _, err := runner.Run(context.Background(), config.Team{Name: "P", JiraProjects: []string{"PT"}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if store.saved.Epics[0].Criterion.Advances != domain.AdvUnscored {
		t.Error("advances should be unscored with no aligner")
	}
}

func TestRunAlignerErrorIsBestEffort(t *testing.T) {
	fetcher := stubFetcher{epics: []ingest.RawEpic{rawEpic("PT-1", "Billing", "Done")}}
	store := &stubStore{}
	runner := newRunner(fetcher, stubSource{}, stubAligner{err: errors.New("boom")}, store)

	if _, err := runner.Run(context.Background(), config.Team{Name: "P", JiraProjects: []string{"PT"}}); err != nil {
		t.Fatalf("run should not fail on aligner error: %v", err)
	}
	if store.saved.Epics[0].Criterion.Advances != domain.AdvUnscored {
		t.Error("advances should be unscored on aligner error")
	}
}

func TestRunFetchErrorPropagates(t *testing.T) {
	fetcher := stubFetcher{err: errors.New("jira down")}
	runner := newRunner(fetcher, stubSource{}, stubAligner{}, &stubStore{})

	if _, err := runner.Run(context.Background(), config.Team{Name: "P", JiraProjects: []string{"PT"}}); err == nil {
		t.Error("expected fetch error to propagate")
	}
}

func TestRunRubricSourceErrorPropagates(t *testing.T) {
	fetcher := stubFetcher{epics: []ingest.RawEpic{rawEpic("PT-1", "Billing", "Done")}}
	runner := newRunner(fetcher, stubSource{err: errors.New("confluence down")}, stubAligner{}, &stubStore{})

	if _, err := runner.Run(context.Background(), config.Team{Name: "P", JiraProjects: []string{"PT"}}); err == nil {
		t.Error("expected rubric source error to propagate")
	}
}

func TestRunIncludesStandaloneIssues(t *testing.T) {
	epic := rawEpic("PT-1", "Billing epic", "In Progress")
	standalone := rawEpic("PT-99", "Standalone bug fix", "To Do")
	fetcher := stubFetcher{
		epics:      []ingest.RawEpic{epic},
		standalone: []ingest.RawEpic{standalone},
	}
	store := &stubStore{}
	runner := newRunner(fetcher, stubSource{}, stubAligner{}, store)

	if _, err := runner.Run(context.Background(), config.Team{Name: "P", JiraProjects: []string{"PT"}}); err != nil {
		t.Fatalf("run: %v", err)
	}

	keys := make(map[string]bool, len(store.saved.Epics))
	for _, e := range store.saved.Epics {
		keys[e.Key] = true
	}
	if !keys["PT-1"] {
		t.Error("epic PT-1 missing from snapshot")
	}
	if !keys["PT-99"] {
		t.Error("standalone PT-99 missing from snapshot")
	}
}
