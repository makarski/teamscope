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
	return complete(ctx, ai, briefPrompt(snap), fmt.Sprintf("team %q", snap.Team))
}

// ExecutiveSummary generates a cross-team executive brief from multiple team
// narratives and their snapshot headlines. Returns empty when no AI is configured.
func ExecutiveSummary(ctx context.Context, ai Completer, snaps []domain.Snapshot) (string, error) {
	if len(snaps) == 0 {
		return "", nil
	}
	return complete(ctx, ai, executivePrompt(snaps), "executive summary")
}

// complete is the shared path for both Brief and ExecutiveSummary: nil-safe
// AI call with trimmed reply.
func complete(ctx context.Context, ai Completer, prompt, label string) (string, error) {
	if ai == nil {
		return "", nil
	}
	reply, err := ai.Complete(ctx, prompt, maxTokens)
	if err != nil {
		return "", fmt.Errorf("narrate: %s: %w", label, err)
	}
	return strings.TrimSpace(reply), nil
}

func briefPrompt(snap domain.Snapshot) string {
	var b strings.Builder
	b.WriteString("You are a product owner writing a progress brief for your team. ")
	b.WriteString("Write as a human would: direct, specific, no bullet-point lists of raw data.\n\n")
	b.WriteString("Structure your brief in two sections:\n")
	b.WriteString("1. SUMMARY (2-3 sentences): What the team is focused on, what's advancing vs stalled, ")
	b.WriteString("and any drift between claimed and actual readiness.\n")
	b.WriteString("2. ACTION PLAN (2-4 numbered items): Concrete next steps — what needs immediate ")
	b.WriteString("attention, what's blocked, what should be deprioritized. Be specific: reference ")
	b.WriteString("ticket keys or pillar names where relevant.\n\n")
	b.WriteString("Plain text, no markdown. Use the section labels SUMMARY: and ACTION PLAN:\n\n")

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

	b.WriteString("\nUnmapped epics (work serving no declared goal):\n")
	b.WriteString(unmappedEpicLines(snap.Epics))

	b.WriteString("\nEpics:\n")
	for _, e := range snap.Epics {
		b.WriteString(epicLine(e))
	}

	// Include GitHub activity if available.
	totalPRs, totalCommits := teamActivity(snap.Epics)
	if totalPRs > 0 || totalCommits > 0 {
		b.WriteString(fmt.Sprintf("\nGitHub activity (last 90 days): %d PRs, %d commits\n", totalPRs, totalCommits))
	}

	return b.String()
}

// teamActivity sums GitHub activity across all epics.
func teamActivity(epics []domain.ClassifiedEpic) (prs, commits int) {
	for _, e := range epics {
		prs += e.Activity.PullRequests
		commits += e.Activity.Commits
	}
	return prs, commits
}

// unmappedEpicLines formats up to 10 unmapped epics for the narrative prompt.
func unmappedEpicLines(epics []domain.ClassifiedEpic) string {
	var b strings.Builder
	count := 0
	for _, e := range epics {
		if e.Criterion.Key != "" {
			continue
		}
		count++
		if count > 10 {
			b.WriteString("  ... (truncated)\n")
			break
		}
		b.WriteString(epicLine(e))
	}
	if count == 0 {
		b.WriteString("  (none)\n")
	}
	return b.String()
}

// epicLine formats one epic for the narrative prompt, including ticket counts.
func epicLine(e domain.ClassifiedEpic) string {
	line := fmt.Sprintf("  - %s %s [%s] criterion=%s advances=%s progress=%.0f%%",
		e.Key, e.Summary, e.Status, e.Criterion.Key, e.Criterion.Advances, e.Progress*100)
	if len(e.Tickets) > 0 {
		done, open := countTicketStatuses(e.Tickets)
		line += fmt.Sprintf(" (%d tickets: %d done, %d open)", len(e.Tickets), done, open)
	}
	return line + "\n"
}

// countTicketStatuses tallies done vs open child tickets.
func countTicketStatuses(tickets []domain.EpicTicket) (done, open int) {
	for _, t := range tickets {
		if t.Status == domain.StatusDone {
			done++
		} else {
			open++
		}
	}
	return done, open
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
