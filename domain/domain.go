package domain

import "time"

// Lens is an optional viewpoint a criterion is judged through. It lets the
// same rubric split into product / business / operations readiness without the
// engine knowing what any specific lens means.
type Lens string

const (
	LensProduct    Lens = "product"
	LensBusiness   Lens = "business"
	LensOperations Lens = "operations"
	LensNone       Lens = ""
)

// Status is a criterion's completion state.
type Status string

const (
	CriterionOpen    Status = "open"
	CriterionDone    Status = "done"
	CriterionUnknown Status = ""
)

// Criterion is a single measurable goal within a rubric. It is deliberately
// generic: it may be a product-readiness pillar, a work-type bucket
// (business/chore/rnd), or anything else a RubricSource yields.
type Criterion struct {
	Key    string  `json:"key"`    // stable identifier, e.g. "security"
	Title  string  `json:"title"`  // human-readable label
	Status Status  `json:"status"` // open | done | unknown
	Weight float64 `json:"weight"` // relative importance, default 1.0
	Lens   Lens    `json:"lens"`
}

// Rubric is the named set of criteria a team is measured against.
type Rubric struct {
	Name     string      `json:"name"`
	Criteria []Criterion `json:"criteria"`
}

// Keys returns the criterion keys in declaration order.
func (r Rubric) Keys() []string {
	keys := make([]string, 0, len(r.Criteria))
	for _, c := range r.Criteria {
		keys = append(keys, c.Key)
	}
	return keys
}

// Find returns the criterion with the given key.
func (r Rubric) Find(key string) (Criterion, bool) {
	for _, c := range r.Criteria {
		if c.Key == key {
			return c, true
		}
	}
	return Criterion{}, false
}

// ClassSource records how an epic was mapped to a criterion, for auditability.
type ClassSource string

const (
	SourceLabel     ClassSource = "label"
	SourceComponent ClassSource = "component"
	SourceKeyword   ClassSource = "keyword"
	SourceAI        ClassSource = "ai"
	SourceUnknown   ClassSource = "unknown"
)

// CriterionRef is the outcome of mapping one epic onto a rubric: which
// criterion it serves, whether it advances that criterion, and how we decided.
type CriterionRef struct {
	Key      string      `json:"key"` // "" when the epic maps to no criterion
	Advances bool        `json:"advances"`
	Source   ClassSource `json:"source"`
	Note     string      `json:"note"`
}

// ProgressStatus buckets an epic's delivery state (reused from roadsnap logic).
type ProgressStatus string

const (
	StatusDone    ProgressStatus = "done"
	StatusOngoing ProgressStatus = "ongoing"
	StatusOverdue ProgressStatus = "overdue"
	StatusToDo    ProgressStatus = "todo"
)

// Activity is the secondary GitHub contribution signal for a team/epic.
type Activity struct {
	PullRequests int `json:"pull_requests"`
	Commits      int `json:"commits"`
}

// ClassifiedEpic is a single epic enriched with its criterion mapping, lens
// and progress.
type ClassifiedEpic struct {
	Key       string         `json:"key"`
	Summary   string         `json:"summary"`
	Criterion CriterionRef   `json:"criterion"`
	Lens      Lens           `json:"lens"`
	Progress  float64        `json:"progress"`
	Status    ProgressStatus `json:"status"`
	Activity  Activity       `json:"activity"`
}

// Snapshot is the point-in-time state for one team, measured against a rubric.
type Snapshot struct {
	ID        int64            `json:"id"`
	Team      string           `json:"team"`
	Rubric    Rubric           `json:"rubric"`
	TakenAt   time.Time        `json:"taken_at"`
	GoalsHash string           `json:"goals_hash"`
	Epics     []ClassifiedEpic `json:"epics"`
}

// Mix returns the share of epics mapped to each criterion key. Shares are
// computed over all epics in the snapshot and sum to 1.0; epics that mapped to
// no criterion are counted under the empty key "".
func (s *Snapshot) Mix() map[string]float64 {
	mix := map[string]float64{}
	total := len(s.Epics)
	if total == 0 {
		return mix
	}
	for _, e := range s.Epics {
		mix[e.Criterion.Key]++
	}
	for k := range mix {
		mix[k] /= float64(total)
	}
	return mix
}
