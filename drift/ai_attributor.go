package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/makarski/teamscope/domain"
)

// AIAttributor uses an LLM to map each ticket to its best-fit criterion.
// Tickets the model can't confidently place are left unattributed (off-rubric).
type AIAttributor struct {
	completer completer
}

// completer performs a single AI completion.
type completer interface {
	Complete(ctx context.Context, prompt string, maxTokens int) (string, error)
}

// maxAttributionTokens caps the attribution reply.
const maxAttributionTokens = 2048

// NewAIAttributor builds an AI-backed attributor. Returns nil when no completer
// is provided, so callers fall back to the deterministic text-proximity path.
func NewAIAttributor(c completer) *AIAttributor {
	if c == nil {
		return nil
	}
	return &AIAttributor{completer: c}
}

// Attribute asks the AI to map each ticket to a criterion key.
func (a *AIAttributor) Attribute(ctx context.Context, tickets []domain.TicketLink, rubric domain.Rubric, texts []string) ([]domain.TicketLink, error) {
	if len(tickets) == 0 {
		return tickets, nil
	}

	reply, err := a.completer.Complete(ctx, attributionPrompt(tickets, rubric, texts), maxAttributionTokens)
	if err != nil {
		return nil, fmt.Errorf("drift: ai attribute: %w", err)
	}

	mapping := decodeAttribution(reply)
	out := make([]domain.TicketLink, len(tickets))
	for i, t := range tickets {
		out[i] = t
		if key, ok := mapping[t.Key]; ok {
			out[i].CriterionKey = key
		}
	}
	return out, nil
}

func attributionPrompt(tickets []domain.TicketLink, rubric domain.Rubric, texts []string) string {
	var b strings.Builder
	b.WriteString("You are mapping Jira tickets to readiness pillars. ")
	b.WriteString("For each ticket, choose the pillar it best serves based on its summary and context. ")
	b.WriteString("If a ticket doesn't clearly serve any pillar, return an empty string.\n\n")

	b.WriteString("Pillars:\n")
	for _, c := range rubric.Criteria {
		b.WriteString(fmt.Sprintf("  %s: %s\n", c.Key, c.Title))
	}

	b.WriteString("\nTickets:\n")
	for _, t := range tickets {
		b.WriteString(fmt.Sprintf("  %s: %s\n", t.Key, t.Summary))
	}

	b.WriteString("\nContext (readiness page / epic descriptions):\n")
	b.WriteString(strings.Join(texts, "\n"))

	b.WriteString("\nReply with JSON only: {\"mappings\":[{\"key\":\"AP-1\",\"criterion\":\"security\"},...]}.")
	return b.String()
}

type attributionReply struct {
	Mappings []struct {
		Key       string `json:"key"`
		Criterion string `json:"criterion"`
	} `json:"mappings"`
}

func decodeAttribution(reply string) map[string]string {
	var parsed attributionReply
	jsonStr := extractJSONFromReply(reply)
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil
	}
	out := make(map[string]string, len(parsed.Mappings))
	for _, m := range parsed.Mappings {
		out[m.Key] = m.Criterion
	}
	return out
}

// extractJSONFromReply pulls the first {...} object out of a reply, tolerating
// any prose the model wraps around it.
func extractJSONFromReply(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if !validJSONBounds(start, end) {
		return raw
	}
	return raw[start : end+1]
}

// validJSONBounds reports whether start/end delimit a non-empty {...} span.
func validJSONBounds(start, end int) bool {
	if start == -1 || end == -1 {
		return false
	}
	return end >= start
}
