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
		Team:    "Payments",
		TakenAt: time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
		Epics: []domain.ClassifiedEpic{
			{Key: "PT-2", Summary: "Upgrade", WorkType: domain.WorkChore, Alignment: domain.AlignOffTrack, AlignNote: "not a goal", Status: domain.StatusOngoing, Progress: 0.2},
			{Key: "PT-1", Summary: "Billing", WorkType: domain.WorkBusiness, Alignment: domain.AlignAligned, Status: domain.StatusOverdue, Progress: 0.5},
		},
	}
}

func TestNewTeamView(t *testing.T) {
	v := NewTeamView(snapshot())

	if v.Team != "Payments" || v.EpicCount != 2 {
		t.Errorf("meta wrong: %+v", v)
	}
	// epics sorted by key
	if v.Epics[0].Key != "PT-1" {
		t.Errorf("epics not sorted: %s first", v.Epics[0].Key)
	}
	// mix 50/50 business/chore
	if v.Mix[0].WorkType != domain.WorkBusiness || v.Mix[0].Percent != 50 {
		t.Errorf("mix wrong: %+v", v.Mix)
	}
	if v.Alignment.Aligned != 1 || v.Alignment.OffTrack != 1 {
		t.Errorf("alignment breakdown wrong: %+v", v.Alignment)
	}
	// PT-1 overdue + PT-2 off-track => both flagged
	if len(v.OffTrack) != 2 {
		t.Errorf("off-track = %d, want 2", len(v.OffTrack))
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

	for _, want := range []string{"Payments", "PT-1", "Billing", "off-track 1", "Needs attention"} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q", want)
		}
	}
	// team with no snapshot is skipped, not errored
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
