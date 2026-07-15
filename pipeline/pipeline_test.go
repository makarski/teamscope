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

type stubFetcher struct {
	epics []ingest.RawEpic
	err   error
}

func (s stubFetcher) FetchEpics(string) ([]ingest.RawEpic, error) { return s.epics, s.err }

type stubClassifier struct{}

func (stubClassifier) Classify(context.Context, *ingest.RawEpic) (domain.WorkType, domain.ClassSource) {
	return domain.WorkBusiness, domain.SourceLabel
}

type stubAligner struct {
	err error
}

func (s stubAligner) Score(context.Context, *ingest.RawEpic) (domain.Alignment, string, error) {
	if s.err != nil {
		return "", "", s.err
	}
	return domain.AlignAligned, "on target", nil
}

type stubStore struct {
	saved domain.Snapshot
}

func (s *stubStore) Save(_ context.Context, snap domain.Snapshot) (int64, error) {
	s.saved = snap
	return 42, nil
}

var statusNames = config.StatusNames{Done: []string{"Done"}, ToDo: []string{"To Do"}}

func newRunner(f Fetcher, a Aligner, store Store) *Runner {
	r := NewRunner(f, stubClassifier{}, a, store, statusNames, "hash1")
	r.now = func() time.Time { return time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC) }
	return r
}

func TestRunEnrichesAndSaves(t *testing.T) {
	fetcher := stubFetcher{epics: []ingest.RawEpic{rawEpic("PT-1", "Billing", "Done")}}
	store := &stubStore{}
	runner := newRunner(fetcher, stubAligner{}, store)

	id, err := runner.Run(context.Background(), config.Team{Name: "Payments", JiraProjects: []string{"PT"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}

	snap := store.saved
	if snap.Team != "Payments" || snap.GoalsHash != "hash1" {
		t.Errorf("snapshot meta wrong: %+v", snap)
	}
	if len(snap.Epics) != 1 {
		t.Fatalf("want 1 epic, got %d", len(snap.Epics))
	}
	e := snap.Epics[0]
	if e.Key != "PT-1" || e.WorkType != domain.WorkBusiness || e.Alignment != domain.AlignAligned {
		t.Errorf("epic enrichment wrong: %+v", e)
	}
	if e.Status != domain.StatusDone {
		t.Errorf("status = %q, want done", e.Status)
	}
}

func TestRunNilAlignerLeavesAlignmentEmpty(t *testing.T) {
	fetcher := stubFetcher{epics: []ingest.RawEpic{rawEpic("PT-1", "Billing", "Done")}}
	store := &stubStore{}
	runner := newRunner(fetcher, nil, store)

	if _, err := runner.Run(context.Background(), config.Team{Name: "P", JiraProjects: []string{"PT"}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := store.saved.Epics[0].Alignment; got != "" {
		t.Errorf("alignment = %q, want empty", got)
	}
}

func TestRunAlignerErrorIsBestEffort(t *testing.T) {
	fetcher := stubFetcher{epics: []ingest.RawEpic{rawEpic("PT-1", "Billing", "Done")}}
	store := &stubStore{}
	runner := newRunner(fetcher, stubAligner{err: errors.New("boom")}, store)

	if _, err := runner.Run(context.Background(), config.Team{Name: "P", JiraProjects: []string{"PT"}}); err != nil {
		t.Fatalf("run should not fail on aligner error: %v", err)
	}
	if got := store.saved.Epics[0].Alignment; got != "" {
		t.Errorf("alignment = %q, want empty on error", got)
	}
}

func TestRunFetchErrorPropagates(t *testing.T) {
	fetcher := stubFetcher{err: errors.New("jira down")}
	runner := newRunner(fetcher, stubAligner{}, &stubStore{})

	if _, err := runner.Run(context.Background(), config.Team{Name: "P", JiraProjects: []string{"PT"}}); err == nil {
		t.Error("expected fetch error to propagate")
	}
}
