package classify

import (
	"sort"
	"strings"

	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/ingest"
)

// keywordRule pairs a lowercased keyword with the criterion key it maps to.
type keywordRule struct {
	word         string
	criterionKey string
}

// RuleEngine maps epics onto a rubric's criteria using Jira metadata in
// priority order: an exact (case-insensitive) label match on a criterion key,
// then a component match, then a configured keyword. It is deterministic and
// needs no AI.
//
// This works well for static rubrics whose criterion keys are meaningful words
// (e.g. business/chore/rnd). For rubrics keyed by opaque ids (e.g. Jira epic
// keys), rules rarely match and the caller falls back to AI mapping.
type RuleEngine struct {
	// canonical maps a lowercased key to the exact rubric-declared key, so
	// matches are case-insensitive but always resolve to a key Rubric.Find
	// (case-sensitive) will accept.
	canonical map[string]string
	keywords  []keywordRule
}

// KeywordHint maps a keyword to a target criterion key.
type KeywordHint struct {
	Keyword      string
	CriterionKey string
}

// NewRuleEngine builds a rule engine for a rubric. keywordHints supply
// text→criterion mappings; the criterion keys themselves are always accepted
// as direct label/component matches. All keys are canonicalized to the exact
// rubric-declared spelling so downstream case-sensitive lookups succeed.
func NewRuleEngine(rubric domain.Rubric, keywordHints []KeywordHint) *RuleEngine {
	canonical := make(map[string]string, len(rubric.Criteria))
	for _, c := range rubric.Criteria {
		canonical[strings.ToLower(c.Key)] = c.Key
	}

	var rules []keywordRule
	for _, h := range keywordHints {
		word := strings.ToLower(strings.TrimSpace(h.Keyword))
		key, ok := canonical[strings.ToLower(strings.TrimSpace(h.CriterionKey))]
		if word == "" || !ok {
			continue
		}
		rules = append(rules, keywordRule{word: word, criterionKey: key})
	}
	// longest word first so more specific keywords win.
	sort.SliceStable(rules, func(i, j int) bool {
		return len(rules[i].word) > len(rules[j].word)
	})

	return &RuleEngine{canonical: canonical, keywords: rules}
}

// Map returns the criterion key an epic maps to and the source that decided it.
// When no rule matches, it returns SourceUnknown so the caller can fall back to
// AI mapping.
func (re *RuleEngine) Map(epic *ingest.RawEpic) (string, domain.ClassSource) {
	if key, ok := re.matchKeys(epic.Labels()); ok {
		return key, domain.SourceLabel
	}
	if key, ok := re.matchKeys(epic.Components()); ok {
		return key, domain.SourceComponent
	}
	if key, ok := re.matchKeywords(strings.ToLower(epic.Text())); ok {
		return key, domain.SourceKeyword
	}
	return "", domain.SourceUnknown
}

// matchKeys recognizes a term that is itself a criterion key (case-insensitive)
// and returns the exact rubric-declared key.
func (re *RuleEngine) matchKeys(terms []string) (string, bool) {
	for _, term := range terms {
		if key, ok := re.canonical[strings.ToLower(strings.TrimSpace(term))]; ok {
			return key, true
		}
	}
	return "", false
}

func (re *RuleEngine) matchKeywords(text string) (string, bool) {
	for _, rule := range re.keywords {
		if strings.Contains(text, rule.word) {
			return rule.criterionKey, true
		}
	}
	return "", false
}
