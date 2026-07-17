package drift

import (
	"context"
	"testing"

	"github.com/makarski/teamscope/domain"
)

type stubFetcher struct {
	tickets map[string]domain.TicketLink
	err     error
}

func (s stubFetcher) FetchByKeys(keys []string) ([]domain.TicketLink, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]domain.TicketLink, 0, len(keys))
	for _, k := range keys {
		if t, ok := s.tickets[k]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func TestExtractKeys(t *testing.T) {
	keys := ExtractKeys("see MARIO-3730 and TTTL-28, also MARIO-3730 again")
	if len(keys) != 2 {
		t.Fatalf("len = %d, want 2: %v", len(keys), keys)
	}
	if keys[0] != "MARIO-3730" {
		t.Errorf("keys[0] = %q, want MARIO-3730", keys[0])
	}
	if keys[1] != "TTTL-28" {
		t.Errorf("keys[1] = %q, want TTTL-28", keys[1])
	}
}

func TestComputeOptimisticDrift(t *testing.T) {
	rubric := domain.Rubric{Criteria: []domain.Criterion{
		{Key: "security", Title: "Security", Status: domain.CriterionDone},
	}}
	fetcher := stubFetcher{tickets: map[string]domain.TicketLink{
		"AP-1": {Key: "AP-1", Status: domain.StatusOngoing},
	}}
	states, err := Compute(context.Background(), fetcher, rubric, []string{"AP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if states[0].Drift != domain.DriftOptimistic {
		t.Errorf("drift = %q, want optimistic", states[0].Drift)
	}
}

func TestComputeStaleDrift(t *testing.T) {
	rubric := domain.Rubric{Criteria: []domain.Criterion{
		{Key: "security", Title: "Security", Status: domain.CriterionOpen},
	}}
	fetcher := stubFetcher{tickets: map[string]domain.TicketLink{
		"AP-1": {Key: "AP-1", Status: domain.StatusDone},
	}}
	states, err := Compute(context.Background(), fetcher, rubric, []string{"AP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if states[0].Drift != domain.DriftStale {
		t.Errorf("drift = %q, want stale", states[0].Drift)
	}
}

func TestComputeNoDrift(t *testing.T) {
	rubric := domain.Rubric{Criteria: []domain.Criterion{
		{Key: "security", Title: "Security", Status: domain.CriterionOpen},
	}}
	fetcher := stubFetcher{tickets: map[string]domain.TicketLink{
		"AP-1": {Key: "AP-1", Status: domain.StatusOngoing},
	}}
	states, err := Compute(context.Background(), fetcher, rubric, []string{"AP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if states[0].Drift != domain.DriftNone {
		t.Errorf("drift = %q, want none", states[0].Drift)
	}
}

func TestComputeNoKeysReturnsEmptyStates(t *testing.T) {
	rubric := domain.Rubric{Criteria: []domain.Criterion{
		{Key: "security", Title: "Security", Status: domain.CriterionOpen},
	}}
	states, err := Compute(context.Background(), stubFetcher{}, rubric, []string{"no keys here"})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("len(states) = %d, want 1", len(states))
	}
	if states[0].Drift != domain.DriftNone {
		t.Errorf("drift = %q, want none", states[0].Drift)
	}
}
