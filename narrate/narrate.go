// Package narrate produces a plain-language progress brief from a snapshot,
// written as a human product owner would report it. The brief covers what the
// team is focused on, what's advancing vs stalled, drift between claimed and
// actual readiness, and what needs attention next.
package narrate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/makarski/teamscope/domain"
)

// Completer performs a single AI completion.
type Completer interface {
	Complete(ctx context.Context, prompt string, maxTokens int) (string, error)
}

// maxTokens caps the narrative reply. A PO brief is a few paragraphs.
const maxTokens = 2048

// Brief generates a per-team progress narrative from a snapshot. The narrative
// is plain text (no markdown) suitable for rendering in a dashboard panel or
// posting to Slack. Returns an empty string when no AI is configured.
func Brief(ctx context.Context, ai Completer, snap domain.Snapshot) (string, error) {
	if ai == nil {
		return "", nil
	}
	reply, err := ai.Complete(ctx, briefPrompt(snap), maxTokens)
	if err != nil {
		return "", fmt.Errorf("narrate: team %q: %w", snap.Team, err)
	}
	return strings.TrimSpace(reply), nil
}

// ExecutiveSummary generates a cross-team executive brief from multiple team
// narratives and their snapshot headlines. Returns empty when no AI is configured.
func ExecutiveSummary(ctx context.Context, ai Completer, snaps []domain.Snapshot) (string, error) {
	if ai == nil {
		return "", nil
	}
	if len(snaps) == 0 {
		return "", nil
	}
	reply, err := ai.Complete(ctx, executivePrompt(snaps), maxTokens)
	if err != nil {
		return "", fmt.Errorf("narrate: executive summary: %w", err)
	}
	return strings.TrimSpace(reply), nil
}

func briefPrompt(snap domain.Snapshot) string {
	var b strings.Builder
	b.WriteString("You are a product owner writing a concise progress brief for your team. ")
	b.WriteString("Write as a human would: direct, specific, no bullet-point lists of raw data. ")
	b.WriteString("Cover: what the team is focused on, what's advancing vs stalled, ")
	b.WriteString("any drift between claimed and actual readiness, and what needs attention next. ")
	b.WriteString("Keep it to 3-5 sentences. Plain text, no markdown.\n\n")

	b.WriteString(fmt.Sprintf("Team: %s\n", snap.Team))
	b.WriteString(fmt.Sprintf("Rubric: %s\n", snap.Rubric.Name))
	b.WriteString(fmt.Sprintf("Snapshot taken: %s\n\n", snap.TakenAt.Format("2006-01-02")))

	b.WriteString("Criteria (pillars and their claimed status):\n")
	for _, c := range snap.Rubric.Criteria {
		b.WriteString(fmt.Sprintf("  - %s (%s): %s\n", c.Title, c.Key, c.Status))
	}

	b.WriteString("\nDrift states:\n")
	for _, s := range snap.States {
		if s.Drift != domain.DriftNone {
			b.WriteString(fmt.Sprintf("  - %s: drift=%s (done=%d open=%d)\n",
				s.Criterion.Title, s.Drift, s.DoneCount, s.OpenCount))
		}
	}

	b.WriteString("\nEpics:\n")
	for _, e := range snap.Epics {
		b.WriteString(fmt.Sprintf("  - %s %s [%s] criterion=%s advances=%s progress=%.0f%%\n",
			e.Key, e.Summary, e.Status, e.Criterion.Key, e.Criterion.Advances, e.Progress*100))
	}

	return b.String()
}

func executivePrompt(snaps []domain.Snapshot) string {
	var b strings.Builder
	b.WriteString("You are a VP of Product writing an executive summary across teams. ")
	b.WriteString("Synthesize the cross-team picture: where things are on track, where they're not, ")
	b.WriteString("what the biggest risks are, and what needs leadership attention. ")
	b.WriteString("Keep it to 4-6 sentences. Plain text, no markdown.\n\n")

	for _, snap := range snaps {
		b.WriteString(fmt.Sprintf("Team: %s (%s, %d epics)\n", snap.Team, snap.Rubric.Name, len(snap.Epics)))
		for _, c := range snap.Rubric.Criteria {
			b.WriteString(fmt.Sprintf("  %s: %s\n", c.Title, c.Status))
		}
		b.WriteString(fmt.Sprintf("  drift: %d pillars\n\n", countDrift(snap.States)))
	}

	return b.String()
}

// countDrift returns the number of pillars with non-none drift.
func countDrift(states []domain.CriterionState) int {
	count := 0
	for _, s := range states {
		if s.Drift != domain.DriftNone {
			count++
		}
	}
	return count
}

// SnapshotJSON serializes a snapshot for debugging/logging.
func SnapshotJSON(snap domain.Snapshot) string {
	data, _ := json.Marshal(snap)
	return string(data)
}
