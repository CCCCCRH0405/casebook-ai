package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schemaV1 = `
CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS members (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  initials TEXT NOT NULL DEFAULT '',
  active INTEGER NOT NULL DEFAULT 1,
  is_admin INTEGER NOT NULL DEFAULT 0,
  pin_hash TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS checklist_items (
  id INTEGER PRIMARY KEY,
  case_id INTEGER NOT NULL REFERENCES cases(id),
  label TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'Needed',
  sort INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_checklist_case ON checklist_items(case_id, sort);
CREATE TABLE IF NOT EXISTS case_types (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  active INTEGER NOT NULL DEFAULT 1,
  sort INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS cases (
  id INTEGER PRIMARY KEY,
  case_no TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  case_type_id INTEGER REFERENCES case_types(id),
  status TEXT NOT NULL DEFAULT 'Open',
  owner_id INTEGER REFERENCES members(id),
  due_date TEXT NOT NULL DEFAULT '',
  deadline TEXT NOT NULL DEFAULT '',
  waiting_on TEXT NOT NULL DEFAULT '',
  waiting_detail TEXT NOT NULL DEFAULT '',
  waiting_since TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  location TEXT NOT NULL DEFAULT '',
  tags TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  created_by INTEGER,
  updated_at TEXT NOT NULL,
  completed_at TEXT NOT NULL DEFAULT '',
  archived_at TEXT NOT NULL DEFAULT '',
  cancelled_reason TEXT NOT NULL DEFAULT '',
  obligation_id INTEGER
);
CREATE TABLE IF NOT EXISTS obligations (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  case_type_id INTEGER REFERENCES case_types(id),
  default_owner_id INTEGER REFERENCES members(id),
  frequency TEXT NOT NULL,
  next_due TEXT NOT NULL,
  lead_days INTEGER NOT NULL DEFAULT 30,
  checklist TEXT NOT NULL DEFAULT '[]',
  active INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  created_by INTEGER
);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY,
  case_id INTEGER NOT NULL REFERENCES cases(id),
  at TEXT NOT NULL,
  actor_id INTEGER,
  kind TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_events_case ON events(case_id, id);
CREATE INDEX IF NOT EXISTS idx_cases_status ON cases(status);
CREATE TABLE IF NOT EXISTS counters (
  year INTEGER PRIMARY KEY,
  next INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS requests (
  id INTEGER PRIMARY KEY,
  case_id INTEGER NOT NULL REFERENCES cases(id),
  kind TEXT NOT NULL,
  from_id INTEGER NOT NULL REFERENCES members(id),
  to_id INTEGER NOT NULL REFERENCES members(id),
  message TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'OPEN',
  created_at TEXT NOT NULL,
  resolved_at TEXT NOT NULL DEFAULT '',
  resolved_by INTEGER
);
CREATE INDEX IF NOT EXISTS idx_requests_to ON requests(to_id, status);
CREATE TABLE IF NOT EXISTS coverage (
  id INTEGER PRIMARY KEY,
  case_id INTEGER NOT NULL REFERENCES cases(id),
  owner_id INTEGER NOT NULL REFERENCES members(id),
  coverer_id INTEGER NOT NULL REFERENCES members(id),
  start_date TEXT NOT NULL,
  end_date TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'ACTIVE',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_coverage_case ON coverage(case_id, status);
`

var defaultTypes = []string{"Inquiry", "Filing", "Testing", "Review", "Remediation", "Request", "Task"}

var waitingOptions = []string{"Business unit", "Legal", "IT", "Regulator", "Vendor", "Counterparty", "Other"}

func openStore(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaV1); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO meta(key,value) VALUES('schema_version','1'),('setup_done','0'),('team_name','')`); err != nil {
		return nil, err
	}
	for i, t := range defaultTypes {
		if _, err := db.Exec(`INSERT OR IGNORE INTO case_types(name,sort) VALUES(?,?)`, t, i); err != nil {
			return nil, err
		}
	}
	// migrations for workspaces created before these columns existed
	db.Exec(`ALTER TABLE members ADD COLUMN pin_hash TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE cases ADD COLUMN obligation_id INTEGER`)
	return db, nil
}

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }
func today() string    { return time.Now().Format("2006-01-02") }

func getMeta(db queryer, key string) string {
	var v string
	_ = db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	return v
}

func setMeta(db execer, key, value string) error {
	_, err := db.Exec(`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

type queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func nextCaseNo(tx *sql.Tx) (string, error) {
	year := time.Now().Year()
	var n int
	err := tx.QueryRow(`SELECT next FROM counters WHERE year=?`, year).Scan(&n)
	if err == sql.ErrNoRows {
		n = 1
		if _, err := tx.Exec(`INSERT INTO counters(year,next) VALUES(?,2)`, year); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	} else {
		if _, err := tx.Exec(`UPDATE counters SET next=next+1 WHERE year=?`, year); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("CB-%d-%04d", year, n), nil
}

func addEvent(tx execer, caseID int64, actorID int64, kind string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var actor any
	if actorID > 0 {
		actor = actorID
	}
	_, err = tx.Exec(`INSERT INTO events(case_id,at,actor_id,kind,payload) VALUES(?,?,?,?,?)`,
		caseID, nowStamp(), actor, kind, string(b))
	return err
}

// runArchiver moves Completed cases past the cool-off window to Archived.
func runArchiver(db *sql.DB, coolOffDays int) {
	cutoff := time.Now().UTC().AddDate(0, 0, -coolOffDays).Format(time.RFC3339)
	rows, err := db.Query(`SELECT id, case_no FROM cases WHERE status='Completed' AND completed_at!='' AND completed_at<=?`, cutoff)
	if err != nil {
		return
	}
	type rec struct {
		id int64
		no string
	}
	var due []rec
	for rows.Next() {
		var r rec
		if rows.Scan(&r.id, &r.no) == nil {
			due = append(due, r)
		}
	}
	rows.Close()
	for _, r := range due {
		tx, err := db.Begin()
		if err != nil {
			return
		}
		_, err = tx.Exec(`UPDATE cases SET status='Archived', archived_at=?, updated_at=? WHERE id=? AND status='Completed'`,
			nowStamp(), nowStamp(), r.id)
		if err == nil {
			err = addEvent(tx, r.id, 0, "ARCHIVED", map[string]any{"auto": true})
		}
		if err == nil {
			tx.Commit()
		} else {
			tx.Rollback()
		}
	}
}
