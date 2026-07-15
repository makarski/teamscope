package classify

import (
	"sort"
	"strings"

	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/ingest"
)

// signalSet holds classification signals from an epic in matching order.
type signalSet struct {
	labels     []string
	components []string
	text       string
}

// keywordRule pairs a lowercased keyword with its work type.
type keywordRule struct {
	word     string
	workType domain.WorkType
}

// RuleEngine classifies epics using Jira metadata in priority order:
// labels, then components, then title/description keywords.
type RuleEngine struct {
	// keywords are evaluated in a deterministic order: by work type
	// (business, chore, rnd) and, within a type, longest word first.
	keywords []keywordRule
}

// NewRuleEngine builds a rule engine from the configured keyword hints.
// Work-type names (business/chore/rnd) are always recognized as labels.
func NewRuleEngine(c *config.Classify) *RuleEngine {
	var rules []keywordRule
	if c != nil {
		rules = appendKeywords(rules, c.Business, domain.WorkBusiness)
		rules = appendKeywords(rules, c.Chore, domain.WorkChore)
		rules = appendKeywords(rules, c.RnD, domain.WorkRnD)
	}
	return &RuleEngine{keywords: rules}
}

func appendKeywords(rules []keywordRule, words []string, wt domain.WorkType) []keywordRule {
	var added []keywordRule
	for _, w := range words {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" {
			added = append(added, keywordRule{word: w, workType: wt})
		}
	}
	// longest word first so more specific keywords win within a type.
	sort.SliceStable(added, func(i, j int) bool {
		return len(added[i].word) > len(added[j].word)
	})
	return append(rules, added...)
}

// Classify returns the work type and the source that decided it. When no rule
// matches, it returns SourceUnknown so the caller can fall back to AI.
func (re *RuleEngine) Classify(epic *ingest.RawEpic) (domain.WorkType, domain.ClassSource) {
	s := signalSet{
		labels:     epic.Labels(),
		components: epic.Components(),
		text:       strings.ToLower(epic.Text()),
	}

	if wt, ok := matchTerms(s.labels); ok {
		return wt, domain.SourceLabel
	}
	if wt, ok := matchTerms(s.components); ok {
		return wt, domain.SourceComponent
	}
	if wt, ok := re.matchKeywords(s.text); ok {
		return wt, domain.SourceKeyword
	}
	return "", domain.SourceUnknown
}

// matchTerms recognizes an explicit work-type name among terms (e.g. a label
// literally named "business" / "chore" / "rnd").
func matchTerms(terms []string) (domain.WorkType, bool) {
	for _, term := range terms {
		wt := domain.WorkType(strings.ToLower(strings.TrimSpace(term)))
		if wt.Valid() {
			return wt, true
		}
	}
	return "", false
}

// matchKeywords scans text for the first configured keyword occurrence,
// evaluated in the engine's deterministic keyword order.
func (re *RuleEngine) matchKeywords(text string) (domain.WorkType, bool) {
	for _, rule := range re.keywords {
		if strings.Contains(text, rule.word) {
			return rule.workType, true
		}
	}
	return "", false
}
