package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/makarski/teamscope/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleSnapshot() domain.Snapshot {
	return domain.Snapshot{
		Team: "Payments",
		Rubric: domain.Rubric{Name: "work", Criteria: []domain.Criterion{
			{Key: "business", Title: "Business", Status: "open", Weight: 1, Lens: domain.LensBusiness},
			{Key: "chore", Title: "Chore", Status: "open", Weight: 1},
		}},
		TakenAt:   time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
		GoalsHash: "abc123",
		Epics: []domain.ClassifiedEpic{
			{
				Key: "PT-1", Summary: "Billing v2",
				Criterion: domain.CriterionRef{
					Key: "business", Source: domain.SourceLabel,
					Advances: domain.AdvAdvances, Note: "matches Q3 billing goal",
				},
				Lens: domain.LensBusiness, Progress: 0.5, Status: domain.StatusOngoing,
				Activity: domain.Activity{PullRequests: 3},
			},
			{
				Key: "PT-2", Summary: "Upgrade deps",
				Criterion: domain.CriterionRef{Key: "chore", Source: domain.SourceKeyword},
				Progress:  1.0, Status: domain.StatusDone,
			},
		},
	}
}

func TestSaveAndLatestRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.Save(ctx, sampleSnapshot())
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero snapshot id")
	}

	got, err := s.Latest(ctx, "Payments")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}

	assertMeta(t, got)
	assertFirstEpic(t, got)
}

func assertMeta(t *testing.T, got domain.Snapshot) {
	t.Helper()
	if got.Team != "Payments" || got.GoalsHash != "abc123" {
		t.Errorf("meta mismatch: %+v", got)
	}
	if got.Rubric.Name != "work" || len(got.Rubric.Criteria) != 2 {
		t.Errorf("rubric not hydrated: %+v", got.Rubric)
	}
	if len(got.Epics) != 2 {
		t.Fatalf("expected 2 epics, got %d", len(got.Epics))
	}
}

func assertFirstEpic(t *testing.T, got domain.Snapshot) {
	t.Helper()
	e := got.Epics[0]
	if e.Key != "PT-1" {
		t.Errorf("key = %q, want PT-1", e.Key)
	}
	if e.Criterion.Key != "business" {
		t.Errorf("criterion key = %q, want business", e.Criterion.Key)
	}
	if e.Criterion.Advances != domain.AdvAdvances {
		t.Error("criterion should advance")
	}
	if e.Lens != domain.LensBusiness {
		t.Errorf("lens mismatch: %q", e.Lens)
	}
	if e.Activity.PullRequests != 3 {
		t.Errorf("activity mismatch: %+v", e.Activity)
	}
}

func TestMix(t *testing.T) {
	snap := sampleSnapshot()
	mix := snap.Mix()
	if mix["business"] != 0.5 || mix["chore"] != 0.5 {
		t.Errorf("unexpected mix: %+v", mix)
	}
}

func TestLatestNoRows(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Latest(context.Background(), "Nobody")
	if err == nil {
		t.Fatal("expected error for missing team")
	}
}

func TestTrendOrdering(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i, day := range []int{10, 12, 11} {
		snap := sampleSnapshot()
		snap.TakenAt = time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC)
		snap.GoalsHash = string(rune('a' + i))
		if _, err := s.Save(ctx, snap); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	trend, err := s.Trend(ctx, "Payments", 10)
	if err != nil {
		t.Fatalf("trend: %v", err)
	}
	if len(trend) != 3 {
		t.Fatalf("expected 3, got %d", len(trend))
	}
	// newest first: day 12, 11, 10
	if trend[0].TakenAt.Day() != 12 || trend[2].TakenAt.Day() != 10 {
		t.Errorf("ordering wrong: %d, %d", trend[0].TakenAt.Day(), trend[2].TakenAt.Day())
	}
}

// TestMigrateDropsGhCommits verifies that a legacy DB with the gh_commits
// column is migrated so inserts (which no longer bind the column) succeed.
func TestMigrateDropsGhCommits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Build a legacy epics table that includes gh_commits NOT NULL.
	_, err = db.Exec(`CREATE TABLE epics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		snapshot_id INTEGER NOT NULL,
		key TEXT NOT NULL,
		summary TEXT NOT NULL,
		criterion_key TEXT NOT NULL,
		class_source TEXT NOT NULL,
		advances TEXT NOT NULL,
		align_note TEXT NOT NULL,
		lens TEXT NOT NULL,
		progress REAL NOT NULL,
		status TEXT NOT NULL,
		gh_prs INTEGER NOT NULL,
		gh_commits INTEGER NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	// Open via the app's Open() which runs migrate().
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open with migrate: %v", err)
	}
	defer s.Close()

	if columnExists(db, "epics", "gh_commits") {
		t.Error("gh_commits should have been dropped by migration")
	}

	// Inserting an epic without gh_commits must succeed.
	ctx := context.Background()
	if _, err := s.Save(ctx, sampleSnapshot()); err != nil {
		t.Fatalf("save after migrate: %v", err)
	}
}
