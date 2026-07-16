package report

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/makarski/teamscope/domain"
)

func snapshot() domain.Snapshot {
	return domain.Snapshot{
		Team: "Payments",
		Rubric: domain.Rubric{Name: "readiness", Criteria: []domain.Criterion{
			{Key: "billing", Title: "Billing", Status: "open", Lens: domain.LensBusiness},
			{Key: "security", Title: "Security", Status: "open", Lens: domain.LensProduct},
			{Key: "telemetry", Title: "Telemetry", Status: "done"},
		}},
		TakenAt: time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
		Epics: []domain.ClassifiedEpic{
			// unmapped chore
			{Key: "PT-2", Summary: "Upgrade", Criterion: domain.CriterionRef{Key: ""}, Status: domain.StatusOngoing, Progress: 0.2},
			// advances billing, but overdue
			{Key: "PT-1", Summary: "Billing", Criterion: domain.CriterionRef{Key: "billing", Advances: true}, Lens: domain.LensBusiness, Status: domain.StatusOverdue, Progress: 0.5},
		},
	}
}

func TestNewTeamViewMeta(t *testing.T) {
	v := NewTeamView(snapshot())
	if v.Team != "Payments" {
		t.Errorf("team = %q, want Payments", v.Team)
	}
	if v.EpicCount != 2 {
		t.Errorf("epic count = %d, want 2", v.EpicCount)
	}
	if v.Rubric != "readiness" {
		t.Errorf("rubric = %q, want readiness", v.Rubric)
	}
	if v.Epics[0].Key != "PT-1" {
		t.Errorf("epics not sorted: %s first", v.Epics[0].Key)
	}
}

func TestNewTeamViewCoverage(t *testing.T) {
	v := NewTeamView(snapshot())
	if len(v.Coverage) != 3 || v.Coverage[0].Key != "billing" {
		t.Fatalf("coverage wrong: %+v", v.Coverage)
	}
	if v.Coverage[0].Advancing != 1 || v.Coverage[0].Total != 1 {
		t.Errorf("billing coverage wrong: %+v", v.Coverage[0])
	}
}

func TestNewTeamViewDrift(t *testing.T) {
	v := NewTeamView(snapshot())
	// security is open with no epics -> drift; telemetry is done -> not drift
	if len(v.Drift) != 1 || v.Drift[0].Key != "security" {
		t.Errorf("drift wrong: %+v", v.Drift)
	}
}

func TestNewTeamViewUnmappedAndOffTrack(t *testing.T) {
	v := NewTeamView(snapshot())
	if len(v.Unmapped) != 1 || v.Unmapped[0].Key != "PT-2" {
		t.Errorf("unmapped wrong: %+v", v.Unmapped)
	}
	if len(v.OffTrack) != 1 || v.OffTrack[0].Key != "PT-1" {
		t.Errorf("off-track wrong: %+v", v.OffTrack)
	}
}

func TestNewTeamViewBlockerFocus(t *testing.T) {
	v := NewTeamView(snapshot())
	// only PT-1 is on an open criterion (PT-2 unmapped) -> 50%
	if v.BlockerFocus != 50 {
		t.Errorf("blocker focus = %d, want 50", v.BlockerFocus)
	}
}

func TestProgressRounding(t *testing.T) {
	v := NewTeamView(snapshot())
	byKey := map[string]int{}
	for _, e := range v.Epics {
		byKey[e.Key] = e.Progress
	}
	if byKey["PT-1"] != 50 || byKey["PT-2"] != 20 {
		t.Errorf("progress rounding wrong: %+v", byKey)
	}
}

type stubSource struct {
	teams []string
	snaps map[string]domain.Snapshot
}

func (s stubSource) Teams(context.Context) ([]string, error) { return s.teams, nil }

func (s stubSource) Latest(_ context.Context, team string) (domain.Snapshot, error) {
	snap, ok := s.snaps[team]
	if !ok {
		return domain.Snapshot{}, sql.ErrNoRows
	}
	return snap, nil
}

func TestWebRender(t *testing.T) {
	src := stubSource{
		teams: []string{"Payments", "Empty"},
		snaps: map[string]domain.Snapshot{"Payments": snapshot()},
	}
	wr, err := NewWebRenderer(src)
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}

	var buf bytes.Buffer
	if err := wr.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	for _, want := range []string{"Payments", "PT-1", "Billing", "blocker focus 50%", "Drift", "security"} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q", want)
		}
	}
	if strings.Contains(html, ">Empty<") {
		t.Error("empty team should be skipped")
	}
}

func TestWebRenderEmpty(t *testing.T) {
	wr, _ := NewWebRenderer(stubSource{teams: nil, snaps: nil})
	var buf bytes.Buffer
	if err := wr.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "No snapshots yet") {
		t.Error("expected empty-state message")
	}
}
