package domain

import "time"

// WorkType is the fixed taxonomy every epic is classified into.
type WorkType string

const (
	WorkBusiness WorkType = "business"
	WorkChore    WorkType = "chore"
	WorkRnD      WorkType = "rnd"
)

func (w WorkType) Valid() bool {
	switch w {
	case WorkBusiness, WorkChore, WorkRnD:
		return true
	}
	return false
}

// AllWorkTypes returns the taxonomy in a stable order for reporting.
func AllWorkTypes() []WorkType {
	return []WorkType{WorkBusiness, WorkChore, WorkRnD}
}

// Alignment scores an epic against the declared goals prompt.
type Alignment string

const (
	AlignAligned  Alignment = "aligned"
	AlignPartial  Alignment = "partial"
	AlignOffTrack Alignment = "off_track"
)

// ClassSource records how a work type was decided, for auditability.
type ClassSource string

const (
	SourceLabel     ClassSource = "label"
	SourceComponent ClassSource = "component"
	SourceKeyword   ClassSource = "keyword"
	SourceAI        ClassSource = "ai"
	SourceUnknown   ClassSource = "unknown"
)

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

// ClassifiedEpic is a single epic enriched with work type, alignment and progress.
type ClassifiedEpic struct {
	Key         string         `json:"key"`
	Summary     string         `json:"summary"`
	WorkType    WorkType       `json:"work_type"`
	ClassSource ClassSource    `json:"class_source"`
	Alignment   Alignment      `json:"alignment"`
	AlignNote   string         `json:"align_note"`
	Progress    float64        `json:"progress"`
	Status      ProgressStatus `json:"status"`
	Activity    Activity       `json:"activity"`
}

// Snapshot is the point-in-time state for one team.
type Snapshot struct {
	ID        int64            `json:"id"`
	Team      string           `json:"team"`
	TakenAt   time.Time        `json:"taken_at"`
	GoalsHash string           `json:"goals_hash"`
	Epics     []ClassifiedEpic `json:"epics"`
}

// Mix returns the share of each work type across the snapshot's epics.
// Shares sum to 1.0 (empty snapshot returns all zeros).
func (s *Snapshot) Mix() map[WorkType]float64 {
	mix := map[WorkType]float64{
		WorkBusiness: 0,
		WorkChore:    0,
		WorkRnD:      0,
	}
	total := len(s.Epics)
	if total == 0 {
		return mix
	}
	for _, e := range s.Epics {
		mix[e.WorkType]++
	}
	for k := range mix {
		mix[k] /= float64(total)
	}
	return mix
}
