// Package drift reconciles a readiness page's hand-set RAG against the live
// status of Jira tickets referenced by each pillar. The verdict is deterministic:
//
//	optimistic — page says done, but tickets are still open
//	stale      — page says gap, but tickets are actually done
//	none       — page and tickets agree
package drift

import (
	"context"
	"fmt"
	"regexp"

	"github.com/makarski/teamscope/domain"
)

// TicketFetcher fetches the live status of Jira issues by key.
type TicketFetcher interface {
	FetchByKeys(keys []string) ([]domain.TicketLink, error)
}

// keyPattern matches Jira issue keys like MARIO-3730, TTTL-28, AP-123.
var keyPattern = regexp.MustCompile(`[A-Z][A-Z0-9]+-\d+`)

// ExtractKeys finds all Jira issue keys referenced in text, de-duplicated and
// preserving first-seen order.
func ExtractKeys(text string) []string {
	matches := keyPattern.FindAllString(text, -1)
	seen := map[string]bool{}
	keys := make([]string, 0, len(matches))
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			keys = append(keys, m)
		}
	}
	return keys
}

// Compute reconciles each criterion's claimed status against the live status of
// the tickets referenced in the provided text (typically the Confluence page
// text and/or the tracking epic's description). It returns one CriterionState
// per criterion, in rubric order.
func Compute(ctx context.Context, fetcher TicketFetcher, rubric domain.Rubric, texts []string) ([]domain.CriterionState, error) {
	allKeys := collectKeys(texts)
	if len(allKeys) == 0 {
		return emptyStates(rubric), nil
	}

	tickets, err := fetcher.FetchByKeys(allKeys)
	if err != nil {
		return nil, fmt.Errorf("drift: fetch tickets: %w", err)
	}

	ticketByKey := map[string]domain.TicketLink{}
	for _, t := range tickets {
		ticketByKey[t.Key] = t
	}

	// Map each ticket to the criterion it belongs to. A ticket belongs to a
	// criterion if the criterion's key appears near the ticket key in the text.
	// For simplicity in this first pass, we attribute all linked tickets to
	// every criterion and compute aggregate drift per criterion from the full
	// set. Per-criterion attribution will follow when the AI labels tickets.
	linked := make([]domain.TicketLink, 0, len(tickets))
	for _, t := range tickets {
		linked = append(linked, t)
	}

	done, open := countDoneOpen(linked)
	states := make([]domain.CriterionState, len(rubric.Criteria))
	for i, c := range rubric.Criteria {
		states[i] = domain.CriterionState{
			Criterion:  c,
			LinkedKeys: linked,
			DoneCount:  done,
			OpenCount:  open,
			Drift:      verdict(c.Status, done, open),
		}
	}
	return states, nil
}

// collectKeys merges keys from all texts, de-duplicated.
func collectKeys(texts []string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, text := range texts {
		for _, k := range ExtractKeys(text) {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	return keys
}

func countDoneOpen(tickets []domain.TicketLink) (done, open int) {
	for _, t := range tickets {
		if t.Status == domain.StatusDone {
			done++
		} else {
			open++
		}
	}
	return done, open
}

// verdict applies the deterministic drift rule:
//
//	page says done + tickets open  → optimistic
//	page says gap  + tickets done  → stale
//	else                           → none
func verdict(claimed domain.Status, done, open int) domain.Drift {
	if claimed == domain.CriterionDone && open > 0 {
		return domain.DriftOptimistic
	}
	if claimed == domain.CriterionOpen && allDone(done, open) {
		return domain.DriftStale
	}
	return domain.DriftNone
}

// allDone reports whether there are completed tickets and none still open.
func allDone(done, open int) bool {
	return done > 0 && open == 0
}

func emptyStates(rubric domain.Rubric) []domain.CriterionState {
	states := make([]domain.CriterionState, len(rubric.Criteria))
	for i, c := range rubric.Criteria {
		states[i] = domain.CriterionState{Criterion: c}
	}
	return states
}
