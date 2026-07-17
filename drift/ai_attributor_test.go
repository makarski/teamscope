package drift

import (
	"context"
	"testing"

	"github.com/makarski/teamscope/domain"
)

type stubCompleter struct {
	reply string
	err   error
}

func (s stubCompleter) Complete(_ context.Context, _ string, _ int) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.reply, nil
}

func TestAIAttributorValidMapping(t *testing.T) {
	rubric := domain.Rubric{Criteria: []domain.Criterion{
		{Key: "security", Title: "Security"},
		{Key: "performance", Title: "Performance"},
	}}
	tickets := []domain.TicketLink{
		{Key: "AP-1", Summary: "Add auth"},
		{Key: "AP-2", Summary: "Cache layer"},
	}
	reply := `{"mappings":[{"key":"AP-1","criterion":"security"},{"key":"AP-2","criterion":"performance"}]}`
	attr := NewAIAttributor(stubCompleter{reply: reply})

	out, err := attr.Attribute(context.Background(), tickets, rubric, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].CriterionKey != "security" {
		t.Errorf("AP-1 criterion = %q, want security", out[0].CriterionKey)
	}
	if out[1].CriterionKey != "performance" {
		t.Errorf("AP-2 criterion = %q, want performance", out[1].CriterionKey)
	}
}

func TestAIAttributorMalformedJSONFallsBack(t *testing.T) {
	rubric := domain.Rubric{Criteria: []domain.Criterion{
		{Key: "security", Title: "Security"},
	}}
	tickets := []domain.TicketLink{
		{Key: "AP-1", Summary: "Add auth"},
	}
	reply := "this is not JSON at all"
	attr := NewAIAttributor(stubCompleter{reply: reply})

	out, err := attr.Attribute(context.Background(), tickets, rubric, []string{"Security: AP-1"})
	if err != nil {
		t.Fatal(err)
	}
	// Should fall back to text attribution
	if out[0].CriterionKey != "security" {
		t.Errorf("AP-1 criterion = %q, want security (text fallback)", out[0].CriterionKey)
	}
}

func TestAIAttributorUnknownCriterionIgnored(t *testing.T) {
	rubric := domain.Rubric{Criteria: []domain.Criterion{
		{Key: "security", Title: "Security"},
	}}
	tickets := []domain.TicketLink{
		{Key: "AP-1", Summary: "Add auth"},
	}
	reply := `{"mappings":[{"key":"AP-1","criterion":"nonexistent"}]}`
	attr := NewAIAttributor(stubCompleter{reply: reply})

	out, err := attr.Attribute(context.Background(), tickets, rubric, []string{"Security: AP-1"})
	if err != nil {
		t.Fatal(err)
	}
	// Unknown criterion should be ignored, falling back to text attribution
	if out[0].CriterionKey != "security" {
		t.Errorf("AP-1 criterion = %q, want security (text fallback after unknown key)", out[0].CriterionKey)
	}
}
