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
	"strings"

	"github.com/makarski/teamscope/domain"
)

// TicketFetcher fetches the live status of Jira issues by key.
type TicketFetcher interface {
	FetchByKeys(keys []string) ([]domain.TicketLink, error)
}

// Attributor maps each ticket to the criterion it serves. Returns the
// criterion key, or "" when the ticket is off-rubric. May be nil — tickets
// are then attributed by text proximity as a deterministic fallback.
type Attributor interface {
	Attribute(ctx context.Context, tickets []domain.TicketLink, rubric domain.Rubric, texts []string) ([]domain.TicketLink, error)
}

// Checker bundles the collaborators needed to compute drift. Attributor may
// be nil, in which case a deterministic text-proximity attributor is used.
type Checker struct {
	Fetcher    TicketFetcher
	Attributor Attributor
}

// NewChecker builds a drift checker from a fetcher and an optional attributor.
func NewChecker(fetcher TicketFetcher, attr Attributor) *Checker {
	return &Checker{Fetcher: fetcher, Attributor: attr}
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

// Compute reconciles each criterion's claimed status against the live status
// of the tickets referenced in the provided text (typically the Confluence
// page text and/or the tracking epic's description). It returns one
// CriterionState per criterion, in rubric order.
//
// If the Checker has an Attributor, it maps each ticket to its best-fit
// criterion. Otherwise, a deterministic text-proximity attributor is used: a
// ticket is attributed to the criterion whose title appears nearest before the
// ticket key in the source text.
func (c *Checker) Compute(ctx context.Context, rubric domain.Rubric, texts []string) ([]domain.CriterionState, error) {
	allKeys := collectKeys(texts)
	if len(allKeys) == 0 {
		return emptyStates(rubric), nil
	}

	tickets, err := c.Fetcher.FetchByKeys(allKeys)
	if err != nil {
		return nil, fmt.Errorf("drift: fetch tickets: %w", err)
	}

	if c.Attributor != nil {
		tickets, err = c.Attributor.Attribute(ctx, tickets, rubric, texts)
		if err != nil {
			return nil, fmt.Errorf("drift: attribute tickets: %w", err)
		}
	} else {
		tickets = attributeByText(tickets, rubric, texts)
	}

	return buildStates(rubric, tickets), nil
}

// buildStates groups tickets by their attributed criterion key and computes
// drift per criterion.
func buildStates(rubric domain.Rubric, tickets []domain.TicketLink) []domain.CriterionState {
	byCriterion := map[string][]domain.TicketLink{}
	for _, t := range tickets {
		byCriterion[t.CriterionKey] = append(byCriterion[t.CriterionKey], t)
	}

	states := make([]domain.CriterionState, len(rubric.Criteria))
	for i, c := range rubric.Criteria {
		linked := byCriterion[c.Key]
		done, open := countDoneOpen(linked)
		states[i] = domain.CriterionState{
			Criterion:  c,
			LinkedKeys: linked,
			DoneCount:  done,
			OpenCount:  open,
			Drift:      verdict(c.Status, done, open),
		}
	}
	return states
}

// attributeByText is the deterministic fallback: for each ticket, find the
// criterion whose title appears closest to the ticket key in the source text.
// Tickets with no nearby criterion title are left unattributed (off-rubric).
func attributeByText(tickets []domain.TicketLink, rubric domain.Rubric, texts []string) []domain.TicketLink {
	combined := strings.Join(texts, "\n")
	out := make([]domain.TicketLink, len(tickets))
	for i, t := range tickets {
		out[i] = t
		out[i].CriterionKey = nearestCriterion(t.Key, combined, rubric)
	}
	return out
}

// nearestCriterion finds the criterion whose title appears closest before key
// in text (as a section header precedes its items). Returns "" when no
// criterion title precedes the key.
func nearestCriterion(key, text string, rubric domain.Rubric) string {
	keyIdx := strings.Index(text, key)
	if keyIdx == -1 {
		return ""
	}
	bestKey := ""
	bestDist := -1
	for _, c := range rubric.Criteria {
		titleIdx := strings.LastIndex(text[:keyIdx], c.Title)
		if titleIdx == -1 {
			continue
		}
		dist := keyIdx - titleIdx
		if bestDist == -1 || dist < bestDist {
			bestDist = dist
			bestKey = c.Key
		}
	}
	return bestKey
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
