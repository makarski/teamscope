package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	rubric        TEXT    NOT NULL,
	taken_at      TEXT    NOT NULL,
	goals_hash    TEXT    NOT NULL,
	narrative     TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_snapshots_team_taken
	ON snapshots(team, taken_at DESC);

CREATE TABLE IF NOT EXISTS criterion_mix (
	snapshot_id   INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
	criterion_key TEXT    NOT NULL,
	share         REAL    NOT NULL,
	PRIMARY KEY (snapshot_id, criterion_key)
);

CREATE TABLE IF NOT EXISTS criteria (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	snapshot_id   INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
	key           TEXT    NOT NULL,
	title         TEXT    NOT NULL,
	status        TEXT    NOT NULL,
	weight        REAL    NOT NULL,
	lens          TEXT    NOT NULL,
	UNIQUE(snapshot_id, key)
);

CREATE INDEX IF NOT EXISTS idx_criteria_snapshot
	ON criteria(snapshot_id);

CREATE TABLE IF NOT EXISTS epics (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	snapshot_id   INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
	key           TEXT    NOT NULL,
	summary       TEXT    NOT NULL,
	criterion_key TEXT    NOT NULL,
	class_source  TEXT    NOT NULL,
	advances      TEXT    NOT NULL,
	align_note    TEXT    NOT NULL,
	lens          TEXT    NOT NULL,
	progress      REAL    NOT NULL,
	status        TEXT    NOT NULL,
	gh_prs        INTEGER NOT NULL,
	gh_commits    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_epics_snapshot
	ON epics(snapshot_id);

CREATE TABLE IF NOT EXISTS epic_tickets (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	epic_id       INTEGER NOT NULL REFERENCES epics(id) ON DELETE CASCADE,
	ticket_key    TEXT    NOT NULL,
	summary       TEXT    NOT NULL,
	status        TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_epic_tickets_epic
	ON epic_tickets(epic_id);

CREATE TABLE IF NOT EXISTS criterion_states (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	snapshot_id   INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
	criterion_key TEXT    NOT NULL,
	done_count    INTEGER NOT NULL,
	open_count    INTEGER NOT NULL,
	drift         TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_criterion_states_snapshot
	ON criterion_states(snapshot_id);

CREATE TABLE IF NOT EXISTS ticket_links (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	state_id      INTEGER NOT NULL REFERENCES criterion_states(id) ON DELETE CASCADE,
	ticket_key    TEXT    NOT NULL,
	ticket_status TEXT    NOT NULL,
	ticket_summary TEXT   NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_ticket_links_state
	ON ticket_links(state_id);
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
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// migrate applies incremental schema changes for databases created before the
// current schema. Each migration is idempotent: it checks whether the column
// exists before altering.
func migrate(db *sql.DB) error {
	migrations := []struct {
		table  string
		column string
		ddl    string
	}{
		{"ticket_links", "ticket_summary", `ALTER TABLE ticket_links ADD COLUMN ticket_summary TEXT NOT NULL DEFAULT ''`},
		{"snapshots", "narrative", `ALTER TABLE snapshots ADD COLUMN narrative TEXT NOT NULL DEFAULT ''`},
	}
	for _, m := range migrations {
		if err := addColumnIfMissing(db, m.table, m.column, m.ddl); err != nil {
			return err
		}
	}
	return nil
}

func addColumnIfMissing(db *sql.DB, table, column, ddl string) error {
	if columnExists(db, table, column) {
		return nil
	}
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("migrate: add %s.%s: %w", table, column, err)
	}
	return nil
}

// columnExists reports whether a column exists on a table.
func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return true // assume exists on error to avoid breaking on edge cases
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return true
		}
		if name == column {
			return true
		}
	}
	return false
}

func (s *Store) Close() error {
	return s.db.Close()
}

const timeLayout = time.RFC3339

// Save writes a snapshot, its criterion mix and epics in a single transaction,
// returning the new id.
func (s *Store) Save(ctx context.Context, snap domain.Snapshot) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()

	snapID, err := insertSnapshot(ctx, tx, snap)
	if err != nil {
		return 0, err
	}
	if err := insertCriteria(ctx, tx, snapID, snap.Rubric.Criteria); err != nil {
		return 0, err
	}
	if err := insertMix(ctx, tx, snapID, snap.Mix()); err != nil {
		return 0, err
	}
	if err := insertEpics(ctx, tx, snapID, snap.Epics); err != nil {
		return 0, err
	}
	if err := insertStates(ctx, tx, snapID, snap.States); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit: %w", err)
	}
	return snapID, nil
}

func insertSnapshot(ctx context.Context, tx *sql.Tx, snap domain.Snapshot) (int64, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO snapshots (team, rubric, taken_at, goals_hash, narrative)
		 VALUES (?, ?, ?, ?, ?)`,
		snap.Team, snap.Rubric.Name,
		snap.TakenAt.UTC().Format(timeLayout), snap.GoalsHash, snap.Narrative,
	)
	if err != nil {
		return 0, fmt.Errorf("store: insert snapshot: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: snapshot id: %w", err)
	}
	return id, nil
}

func insertCriteria(ctx context.Context, tx *sql.Tx, snapID int64, criteria []domain.Criterion) error {
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO criteria (snapshot_id, key, title, status, weight, lens)
		 VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: prepare criteria insert: %w", err)
	}
	defer stmt.Close()

	for _, c := range criteria {
		if _, err := stmt.ExecContext(ctx,
			snapID, c.Key, c.Title, string(c.Status), c.Weight, string(c.Lens),
		); err != nil {
			return fmt.Errorf("store: insert criterion %q: %w", c.Key, err)
		}
	}
	return nil
}

func insertMix(ctx context.Context, tx *sql.Tx, snapID int64, mix map[string]float64) error {
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO criterion_mix (snapshot_id, criterion_key, share) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: prepare mix insert: %w", err)
	}
	defer stmt.Close()

	for key, share := range mix {
		if _, err := stmt.ExecContext(ctx, snapID, key, share); err != nil {
			return fmt.Errorf("store: insert mix %q: %w", key, err)
		}
	}
	return nil
}

func insertEpics(ctx context.Context, tx *sql.Tx, snapID int64, epics []domain.ClassifiedEpic) error {
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO epics
		 (snapshot_id, key, summary, criterion_key, class_source, advances, align_note, lens, progress, status, gh_prs, gh_commits)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: prepare epic insert: %w", err)
	}
	defer stmt.Close()

	ticketStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO epic_tickets (epic_id, ticket_key, summary, status) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: prepare ticket insert: %w", err)
	}
	defer ticketStmt.Close()

	for _, e := range epics {
		res, err := stmt.ExecContext(ctx,
			snapID, e.Key, e.Summary, e.Criterion.Key, string(e.Criterion.Source),
			string(e.Criterion.Advances), e.Criterion.Note, string(e.Lens),
			e.Progress, string(e.Status), e.Activity.PullRequests, e.Activity.Commits,
		)
		if err != nil {
			return fmt.Errorf("store: insert epic %s: %w", e.Key, err)
		}
		epicID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("store: epic id: %w", err)
		}
		if err := insertEpicTickets(ctx, ticketStmt, epicID, e.Tickets); err != nil {
			return err
		}
	}
	return nil
}

func insertEpicTickets(ctx context.Context, stmt *sql.Stmt, epicID int64, tickets []domain.EpicTicket) error {
	for _, t := range tickets {
		if _, err := stmt.ExecContext(ctx, epicID, t.Key, t.Summary, string(t.Status)); err != nil {
			return fmt.Errorf("store: insert ticket %s: %w", t.Key, err)
		}
	}
	return nil
}

func insertStates(ctx context.Context, tx *sql.Tx, snapID int64, states []domain.CriterionState) error {
	stateStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO criterion_states (snapshot_id, criterion_key, done_count, open_count, drift)
		 VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: prepare state insert: %w", err)
	}
	defer stateStmt.Close()

	ticketStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO ticket_links (state_id, ticket_key, ticket_status, ticket_summary) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: prepare ticket insert: %w", err)
	}
	defer ticketStmt.Close()

	for _, s := range states {
		stateID, err := insertOneState(ctx, stateStmt, snapID, s)
		if err != nil {
			return err
		}
		if err := insertTickets(ctx, ticketStmt, stateID, s.LinkedKeys); err != nil {
			return err
		}
	}
	return nil
}

func insertOneState(ctx context.Context, stmt *sql.Stmt, snapID int64, s domain.CriterionState) (int64, error) {
	res, err := stmt.ExecContext(ctx,
		snapID, s.Criterion.Key, s.DoneCount, s.OpenCount, string(s.Drift),
	)
	if err != nil {
		return 0, fmt.Errorf("store: insert state %q: %w", s.Criterion.Key, err)
	}
	stateID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: state id: %w", err)
	}
	return stateID, nil
}

func insertTickets(ctx context.Context, stmt *sql.Stmt, stateID int64, tickets []domain.TicketLink) error {
	for _, t := range tickets {
		if _, err := stmt.ExecContext(ctx, stateID, t.Key, string(t.Status), t.Summary); err != nil {
			return fmt.Errorf("store: insert ticket %s: %w", t.Key, err)
		}
	}
	return nil
}

// Latest returns the most recent snapshot for a team, or sql.ErrNoRows if none exist.
func (s *Store) Latest(ctx context.Context, team string) (domain.Snapshot, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, team, rubric, taken_at, goals_hash, narrative
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

	criteria, err := s.loadCriteria(ctx, snap.ID)
	if err != nil {
		return domain.Snapshot{}, err
	}
	snap.Rubric.Criteria = criteria

	states, err := s.loadStates(ctx, snap.ID, criteria)
	if err != nil {
		return domain.Snapshot{}, err
	}
	snap.States = states
	return snap, nil
}

// Trend returns up to n most recent snapshots for a team, newest first.
// Epics are not hydrated; use Latest for full detail.
func (s *Store) Trend(ctx context.Context, team string, n int) ([]domain.Snapshot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, team, rubric, taken_at, goals_hash, narrative
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
	if err := sc.Scan(&snap.ID, &snap.Team, &snap.Rubric.Name, &takenAt, &snap.GoalsHash, &snap.Narrative); err != nil {
		return domain.Snapshot{}, err
	}
	t, err := time.Parse(timeLayout, takenAt)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("store: parse taken_at %q: %w", takenAt, err)
	}
	snap.TakenAt = t
	return snap, nil
}

func (s *Store) loadCriteria(ctx context.Context, snapID int64) ([]domain.Criterion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, title, status, weight, lens
		 FROM criteria WHERE snapshot_id = ? ORDER BY id`, snapID)
	if err != nil {
		return nil, fmt.Errorf("store: query criteria: %w", err)
	}
	defer rows.Close()

	var out []domain.Criterion
	for rows.Next() {
		var (
			c            domain.Criterion
			status, lens string
		)
		if err := rows.Scan(&c.Key, &c.Title, &status, &c.Weight, &lens); err != nil {
			return nil, fmt.Errorf("store: scan criterion: %w", err)
		}
		c.Status = domain.Status(status)
		c.Lens = domain.Lens(lens)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) loadEpics(ctx context.Context, snapID int64) ([]domain.ClassifiedEpic, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, key, summary, criterion_key, class_source, advances, align_note, lens, progress, status, gh_prs, gh_commits
		 FROM epics WHERE snapshot_id = ? ORDER BY id`, snapID)
	if err != nil {
		return nil, fmt.Errorf("store: query epics: %w", err)
	}
	defer rows.Close()

	var out []domain.ClassifiedEpic
	epicIDs := make([]int64, 0)
	for rows.Next() {
		var epicID int64
		e, err := scanEpicWithID(rows, &epicID)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		epicIDs = append(epicIDs, epicID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(out) > 0 {
		attachEpicTickets(ctx, s, out, epicIDs)
	}
	return out, nil
}

func attachEpicTickets(ctx context.Context, s *Store, out []domain.ClassifiedEpic, epicIDs []int64) {
	tickets, err := s.loadAllEpicTickets(ctx, epicIDs)
	if err != nil {
		return
	}
	for i := range out {
		out[i].Tickets = tickets[epicIDs[i]]
	}
}

func scanEpicWithID(rows *sql.Rows, epicID *int64) (domain.ClassifiedEpic, error) {
	var (
		e                                domain.ClassifiedEpic
		critKey, src, lens, st, advances string
	)
	if err := rows.Scan(
		epicID, &e.Key, &e.Summary, &critKey, &src, &advances, &e.Criterion.Note,
		&lens, &e.Progress, &st, &e.Activity.PullRequests, &e.Activity.Commits,
	); err != nil {
		return domain.ClassifiedEpic{}, fmt.Errorf("store: scan epic: %w", err)
	}
	e.Criterion.Key = critKey
	e.Criterion.Source = domain.ClassSource(src)
	e.Criterion.Advances = domain.Advancement(advances)
	e.Lens = domain.Lens(lens)
	e.Status = domain.ProgressStatus(st)
	return e, nil
}

func (s *Store) loadAllEpicTickets(ctx context.Context, epicIDs []int64) (map[int64][]domain.EpicTicket, error) {
	if len(epicIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(epicIDs))
	args := make([]any, len(epicIDs))
	for i, id := range epicIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(
		`SELECT epic_id, ticket_key, summary, status FROM epic_tickets WHERE epic_id IN (%s) ORDER BY id`,
		strings.Join(placeholders, ", "),
	)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query epic tickets: %w", err)
	}
	defer rows.Close()

	out := make(map[int64][]domain.EpicTicket)
	for rows.Next() {
		var (
			epicID int64
			t      domain.EpicTicket
			st     string
		)
		if err := rows.Scan(&epicID, &t.Key, &t.Summary, &st); err != nil {
			return nil, fmt.Errorf("store: scan epic ticket: %w", err)
		}
		t.Status = domain.ProgressStatus(st)
		out[epicID] = append(out[epicID], t)
	}
	return out, rows.Err()
}

func (s *Store) loadStates(ctx context.Context, snapID int64, criteria []domain.Criterion) ([]domain.CriterionState, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT cs.id, cs.criterion_key, cs.done_count, cs.open_count, cs.drift,
		        tl.ticket_key, tl.ticket_status, tl.ticket_summary
		 FROM criterion_states cs
		 LEFT JOIN ticket_links tl ON tl.state_id = cs.id
		 WHERE cs.snapshot_id = ?
		 ORDER BY cs.id, tl.id`, snapID)
	if err != nil {
		return nil, fmt.Errorf("store: query states: %w", err)
	}
	defer rows.Close()

	critByKey := map[string]domain.Criterion{}
	for _, c := range criteria {
		critByKey[c.Key] = c
	}

	var out []domain.CriterionState
	stateByID := map[int64]*domain.CriterionState{}
	for rows.Next() {
		if err := scanStateRow(rows, critByKey, &out, stateByID); err != nil {
			return nil, err
		}
	}
	return out, rows.Err()
}

func scanStateRow(rows *sql.Rows, critByKey map[string]domain.Criterion, out *[]domain.CriterionState, stateByID map[int64]*domain.CriterionState) error {
	var (
		stateID       int64
		critKey       string
		drift         string
		ticketKey     sql.NullString
		ticketStat    sql.NullString
		ticketSummary sql.NullString
	)
	if err := rows.Scan(&stateID, &critKey, new(int), new(int), &drift, &ticketKey, &ticketStat, &ticketSummary); err != nil {
		return fmt.Errorf("store: scan state: %w", err)
	}

	existing, ok := stateByID[stateID]
	if !ok {
		crit, found := critByKey[critKey]
		if !found {
			return nil
		}
		*out = append(*out, domain.CriterionState{
			Criterion: crit,
			Drift:     domain.Drift(drift),
		})
		existing = &(*out)[len(*out)-1]
		stateByID[stateID] = existing
	}

	if !ticketKey.Valid {
		return nil
	}
	status := domain.ProgressStatus(ticketStat.String)
	existing.LinkedKeys = append(existing.LinkedKeys, domain.TicketLink{
		Key:     ticketKey.String,
		Summary: ticketSummary.String,
		Status:  status,
	})
	if status == domain.StatusDone {
		existing.DoneCount++
	} else {
		existing.OpenCount++
	}
	return nil
}
