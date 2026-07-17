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

func testChecker(fetcher TicketFetcher) *Checker {
	return NewChecker(fetcher, nil)
}

func TestComputeDriftVerdicts(t *testing.T) {
	tests := []struct {
		name      string
		status    domain.Status
		ticketSt  domain.ProgressStatus
		wantDrift domain.Drift
	}{
		{"optimistic", domain.CriterionDone, domain.StatusOngoing, domain.DriftOptimistic},
		{"stale", domain.CriterionOpen, domain.StatusDone, domain.DriftStale},
		{"none", domain.CriterionOpen, domain.StatusOngoing, domain.DriftNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rubric := domain.Rubric{Criteria: []domain.Criterion{
				{Key: "security", Title: "Security", Status: tt.status},
			}}
			fetcher := stubFetcher{tickets: map[string]domain.TicketLink{
				"AP-1": {Key: "AP-1", Status: tt.ticketSt},
			}}
			states, err := testChecker(fetcher).Compute(context.Background(), rubric, []string{"Security: AP-1"})
			if err != nil {
				t.Fatal(err)
			}
			if states[0].Drift != tt.wantDrift {
				t.Errorf("drift = %q, want %q", states[0].Drift, tt.wantDrift)
			}
		})
	}
}

func TestComputeNoKeysReturnsEmptyStates(t *testing.T) {
	rubric := domain.Rubric{Criteria: []domain.Criterion{
		{Key: "security", Title: "Security", Status: domain.CriterionOpen},
	}}
	states, err := testChecker(stubFetcher{}).Compute(context.Background(), rubric, []string{"no keys here"})
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

func TestPerCriterionAttribution(t *testing.T) {
	rubric := domain.Rubric{Criteria: []domain.Criterion{
		{Key: "security", Title: "Security", Status: domain.CriterionOpen},
		{Key: "performance", Title: "Performance", Status: domain.CriterionOpen},
	}}
	fetcher := stubFetcher{tickets: map[string]domain.TicketLink{
		"AP-1": {Key: "AP-1", Status: domain.StatusDone},
		"AP-2": {Key: "AP-2", Status: domain.StatusOngoing},
	}}
	texts := []string{"Security pillar: AP-1 is done\nPerformance pillar: AP-2 in progress"}
	states, err := testChecker(fetcher).Compute(context.Background(), rubric, texts)
	if err != nil {
		t.Fatal(err)
	}

	// Security: AP-1 done, no open → stale (page says open, tickets done)
	if states[0].Drift != domain.DriftStale {
		t.Errorf("security drift = %q, want stale", states[0].Drift)
	}
	if len(states[0].LinkedKeys) != 1 {
		t.Fatalf("security linked = %d, want 1", len(states[0].LinkedKeys))
	}
	if states[0].LinkedKeys[0].Key != "AP-1" {
		t.Errorf("security linked key = %q, want AP-1", states[0].LinkedKeys[0].Key)
	}

	// Performance: AP-2 ongoing, no done → none
	if states[1].Drift != domain.DriftNone {
		t.Errorf("performance drift = %q, want none", states[1].Drift)
	}
	if len(states[1].LinkedKeys) != 1 {
		t.Fatalf("performance linked = %d, want 1", len(states[1].LinkedKeys))
	}
	if states[1].LinkedKeys[0].Key != "AP-2" {
		t.Errorf("performance linked key = %q, want AP-2", states[1].LinkedKeys[0].Key)
	}
}
