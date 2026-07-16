package goals

import (
	"context"
	"errors"
	"testing"

	"github.com/makarski/teamscope/domain"
)

func TestStaticSourceDefaultsWeight(t *testing.T) {
	src := NewStaticSource(domain.Rubric{Name: "work", Criteria: []domain.Criterion{
		{Key: "business", Title: "Business"},
		{Key: "chore", Title: "Chore", Weight: 2},
	}})

	r, err := src.Rubric(context.Background(), "any-team")
	if err != nil {
		t.Fatalf("Rubric: %v", err)
	}
	if r.Name != "work" || len(r.Criteria) != 2 {
		t.Fatalf("unexpected rubric: %+v", r)
	}
	if r.Criteria[0].Weight != 1.0 {
		t.Errorf("default weight = %v, want 1.0", r.Criteria[0].Weight)
	}
	if r.Criteria[1].Weight != 2.0 {
		t.Errorf("explicit weight = %v, want 2.0", r.Criteria[1].Weight)
	}
}

type stubFetcher struct {
	epics []LabelledEpic
	err   error
}

func (s stubFetcher) FetchByLabel(_, _ string) ([]LabelledEpic, error) {
	return s.epics, s.err
}

func TestJiraLabelSourceMapsEpicsToCriteria(t *testing.T) {
	fetcher := stubFetcher{epics: []LabelledEpic{
		{Key: "AP-942", Summary: "Feature readiness", Done: false},
		{Key: "AP-945", Summary: "Security readiness", Done: true},
	}}
	src := NewJiraLabelSource(fetcher, "AP", "product-readiness", Binding{Name: "readiness", Lens: domain.LensProduct})

	r, err := src.Rubric(context.Background(), "Access Manager")
	if err != nil {
		t.Fatalf("Rubric: %v", err)
	}
	assertReadinessRubric(t, r, "AP-942", "Feature readiness")
}

func TestJiraLabelSourcePropagatesError(t *testing.T) {
	src := NewJiraLabelSource(stubFetcher{err: errors.New("boom")}, "AP", "x", Binding{Name: "r"})
	if _, err := src.Rubric(context.Background(), "t"); err == nil {
		t.Fatal("expected error")
	}
}

type stubPageFetcher struct {
	pillars []Pillar
	err     error
}

func (s stubPageFetcher) FetchReadinessPillars(string) ([]Pillar, error) {
	return s.pillars, s.err
}

func TestConfluenceSourceMapsPillarsToCriteria(t *testing.T) {
	fetcher := stubPageFetcher{pillars: []Pillar{
		{Key: "feature-readiness", Title: "Feature Readiness", Done: false},
		{Key: "security-compliance", Title: "Security & Compliance", Done: true},
	}}
	src := NewConfluenceSource(fetcher, "1158283406", Binding{Name: "readiness", Lens: domain.LensProduct})

	r, err := src.Rubric(context.Background(), "Access Manager")
	if err != nil {
		t.Fatalf("Rubric: %v", err)
	}
	assertReadinessRubric(t, r, "feature-readiness", "Feature Readiness")
}

// assertReadinessRubric checks a two-criterion readiness rubric: the first
// criterion is open with the given key/title on the product lens, the second
// is done.
func assertReadinessRubric(t *testing.T, r domain.Rubric, wantKey, wantTitle string) {
	t.Helper()
	if r.Name != "readiness" || len(r.Criteria) != 2 {
		t.Fatalf("unexpected rubric: %+v", r)
	}
	first := r.Criteria[0]
	if first.Key != wantKey {
		t.Errorf("criterion 0 key = %q, want %q", first.Key, wantKey)
	}
	if first.Title != wantTitle {
		t.Errorf("criterion 0 title = %q, want %q", first.Title, wantTitle)
	}
	if first.Status != "open" {
		t.Errorf("criterion 0 status = %q, want open", first.Status)
	}
	if first.Lens != domain.LensProduct {
		t.Errorf("criterion 0 lens = %q, want product", first.Lens)
	}
	if got := r.Criteria[1].Status; got != "done" {
		t.Errorf("criterion 1 status = %q, want done", got)
	}
}

func TestConfluenceSourcePropagatesError(t *testing.T) {
	src := NewConfluenceSource(stubPageFetcher{err: errors.New("boom")}, "1", Binding{Name: "r"})
	if _, err := src.Rubric(context.Background(), "t"); err == nil {
		t.Fatal("expected error")
	}
}
