package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/makarski/teamscope/domain"
)

// Store persists team snapshots in SQLite.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS snapshots (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	team          TEXT    NOT NULL,
	taken_at      TEXT    NOT NULL,
	goals_hash    TEXT    NOT NULL,
	pct_business  REAL    NOT NULL,
	pct_chore     REAL    NOT NULL,
	pct_rnd       REAL    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_snapshots_team_taken
	ON snapshots(team, taken_at DESC);

CREATE TABLE IF NOT EXISTS epics (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	snapshot_id   INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
	key           TEXT    NOT NULL,
	summary       TEXT    NOT NULL,
	work_type     TEXT    NOT NULL,
	class_source  TEXT    NOT NULL,
	alignment     TEXT    NOT NULL,
	align_note    TEXT    NOT NULL,
	progress      REAL    NOT NULL,
	status        TEXT    NOT NULL,
	gh_prs        INTEGER NOT NULL,
	gh_commits    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_epics_snapshot
	ON epics(snapshot_id);
`

// Open opens (creating if needed) the SQLite database at path and applies migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: enable foreign keys: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

const timeLayout = time.RFC3339

// Save writes a snapshot and its epics in a single transaction, returning the new id.
func (s *Store) Save(ctx context.Context, snap domain.Snapshot) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()

	mix := snap.Mix()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO snapshots (team, taken_at, goals_hash, pct_business, pct_chore, pct_rnd)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		snap.Team,
		snap.TakenAt.UTC().Format(timeLayout),
		snap.GoalsHash,
		mix[domain.WorkBusiness],
		mix[domain.WorkChore],
		mix[domain.WorkRnD],
	)
	if err != nil {
		return 0, fmt.Errorf("store: insert snapshot: %w", err)
	}

	snapID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: snapshot id: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO epics
		 (snapshot_id, key, summary, work_type, class_source, alignment, align_note, progress, status, gh_prs, gh_commits)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("store: prepare epic insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range snap.Epics {
		if _, err := stmt.ExecContext(ctx,
			snapID, e.Key, e.Summary, string(e.WorkType), string(e.ClassSource),
			string(e.Alignment), e.AlignNote, e.Progress, string(e.Status),
			e.Activity.PullRequests, e.Activity.Commits,
		); err != nil {
			return 0, fmt.Errorf("store: insert epic %s: %w", e.Key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit: %w", err)
	}
	return snapID, nil
}

// Latest returns the most recent snapshot for a team, or sql.ErrNoRows if none exist.
func (s *Store) Latest(ctx context.Context, team string) (domain.Snapshot, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, team, taken_at, goals_hash
		 FROM snapshots WHERE team = ? ORDER BY taken_at DESC LIMIT 1`, team)

	snap, err := scanSnapshotMeta(row)
	if err != nil {
		return domain.Snapshot{}, err
	}

	epics, err := s.loadEpics(ctx, snap.ID)
	if err != nil {
		return domain.Snapshot{}, err
	}
	snap.Epics = epics
	return snap, nil
}

// Trend returns up to n most recent snapshots for a team, newest first.
// Epics are not hydrated; use Latest for full detail.
func (s *Store) Trend(ctx context.Context, team string, n int) ([]domain.Snapshot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, team, taken_at, goals_hash
		 FROM snapshots WHERE team = ? ORDER BY taken_at DESC LIMIT ?`, team, n)
	if err != nil {
		return nil, fmt.Errorf("store: query trend: %w", err)
	}
	defer rows.Close()

	var out []domain.Snapshot
	for rows.Next() {
		snap, err := scanSnapshotMeta(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

// Teams returns the distinct team names that have at least one snapshot,
// ordered alphabetically.
func (s *Store) Teams(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT team FROM snapshots ORDER BY team`)
	if err != nil {
		return nil, fmt.Errorf("store: query teams: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var team string
		if err := rows.Scan(&team); err != nil {
			return nil, fmt.Errorf("store: scan team: %w", err)
		}
		out = append(out, team)
	}
	return out, rows.Err()
}

// scanner unifies *sql.Row and *sql.Rows for meta scanning.
type scanner interface {
	Scan(dest ...any) error
}

func scanSnapshotMeta(sc scanner) (domain.Snapshot, error) {
	var (
		snap    domain.Snapshot
		takenAt string
	)
	if err := sc.Scan(&snap.ID, &snap.Team, &takenAt, &snap.GoalsHash); err != nil {
		return domain.Snapshot{}, err
	}
	t, err := time.Parse(timeLayout, takenAt)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("store: parse taken_at %q: %w", takenAt, err)
	}
	snap.TakenAt = t
	return snap, nil
}

func (s *Store) loadEpics(ctx context.Context, snapID int64) ([]domain.ClassifiedEpic, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, summary, work_type, class_source, alignment, align_note, progress, status, gh_prs, gh_commits
		 FROM epics WHERE snapshot_id = ? ORDER BY id`, snapID)
	if err != nil {
		return nil, fmt.Errorf("store: query epics: %w", err)
	}
	defer rows.Close()

	var out []domain.ClassifiedEpic
	for rows.Next() {
		var (
			e               domain.ClassifiedEpic
			wt, src, al, st string
		)
		if err := rows.Scan(
			&e.Key, &e.Summary, &wt, &src, &al, &e.AlignNote,
			&e.Progress, &st, &e.Activity.PullRequests, &e.Activity.Commits,
		); err != nil {
			return nil, fmt.Errorf("store: scan epic: %w", err)
		}
		e.WorkType = domain.WorkType(wt)
		e.ClassSource = domain.ClassSource(src)
		e.Alignment = domain.Alignment(al)
		e.Status = domain.ProgressStatus(st)
		out = append(out, e)
	}
	return out, rows.Err()
}
