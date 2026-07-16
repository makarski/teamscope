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
	src := NewJiraLabelSource(fetcher, "AP", "product-readiness", "readiness", domain.LensProduct)

	r, err := src.Rubric(context.Background(), "Access Manager")
	if err != nil {
		t.Fatalf("Rubric: %v", err)
	}
	if r.Name != "readiness" || len(r.Criteria) != 2 {
		t.Fatalf("unexpected rubric: %+v", r)
	}

	first := r.Criteria[0]
	if first.Key != "AP-942" || first.Title != "Feature readiness" || first.Status != "open" {
		t.Errorf("criterion 0 = %+v", first)
	}
	if first.Lens != domain.LensProduct {
		t.Errorf("lens = %q, want product", first.Lens)
	}
	if got := r.Criteria[1].Status; got != "done" {
		t.Errorf("criterion 1 status = %q, want done", got)
	}
}

func TestJiraLabelSourcePropagatesError(t *testing.T) {
	src := NewJiraLabelSource(stubFetcher{err: errors.New("boom")}, "AP", "x", "r", domain.LensNone)
	if _, err := src.Rubric(context.Background(), "t"); err == nil {
		t.Fatal("expected error")
	}
}
