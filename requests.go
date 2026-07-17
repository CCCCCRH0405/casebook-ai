package main

import (
	"net/http"
	"strconv"
	"strings"
)

type requestRow struct {
	ID         int64  `json:"id"`
	CaseNo     string `json:"case_no"`
	CaseTitle  string `json:"case_title"`
	Kind       string `json:"kind"`
	FromID     int64  `json:"from_id"`
	FromName   string `json:"from_name"`
	ToID       int64  `json:"to_id"`
	ToName     string `json:"to_name"`
	Message    string `json:"message"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	ResolvedAt string `json:"resolved_at"`
}

const reqSelect = `SELECT r.id,c.case_no,c.title,r.kind,r.from_id,mf.name,r.to_id,mt.name,r.message,r.status,r.created_at,r.resolved_at
FROM requests r JOIN cases c ON c.id=r.case_id
JOIN members mf ON mf.id=r.from_id JOIN members mt ON mt.id=r.to_id`

func scanRequests(rows interface {
	Next() bool
	Scan(...any) error
}) []requestRow {
	out := []requestRow{}
	for rows.Next() {
		var r requestRow
		if rows.Scan(&r.ID, &r.CaseNo, &r.CaseTitle, &r.Kind, &r.FromID, &r.FromName, &r.ToID, &r.ToName,
			&r.Message, &r.Status, &r.CreatedAt, &r.ResolvedAt) == nil {
			out = append(out, r)
		}
	}
	return out
}

func (s *server) openRequestsForCase(caseID int64) []requestRow {
	rows, err := s.db.Query(reqSelect+` WHERE r.case_id=? AND r.status='OPEN' ORDER BY r.id`, caseID)
	if err != nil {
		return []requestRow{}
	}
	defer rows.Close()
	return scanRequests(rows)
}

var requestKinds = map[string]bool{"COVERAGE": true, "TRANSFER": true, "HELP": true}

func (s *server) handleCreateRequest(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	c, err := s.caseByNo(r.PathValue("no"))
	if err != nil {
		writeErr(w, 404, "case not found")
		return
	}
	var req struct {
		Kind    string `json:"kind"`
		ToID    int64  `json:"to_id"`
		Message string `json:"message"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	req.Kind = strings.ToUpper(strings.TrimSpace(req.Kind))
	req.Message = strings.TrimSpace(req.Message)
	if !requestKinds[req.Kind] {
		writeErr(w, 400, "unknown request kind")
		return
	}
	if isClosed(c.Status) {
		writeErr(w, 409, "this case is closed")
		return
	}
	switch req.Kind {
	case "COVERAGE":
		if c.OwnerID != actor.ID {
			writeErr(w, 403, "only the owner can ask for coverage")
			return
		}
	case "TRANSFER":
		if c.OwnerID == 0 {
			writeErr(w, 409, "this case is unassigned — just claim it")
			return
		}
		if c.OwnerID == actor.ID {
			writeErr(w, 409, "you already own this case")
			return
		}
		req.ToID = c.OwnerID
	}
	if req.ToID == actor.ID {
		writeErr(w, 400, "you cannot send a request to yourself")
		return
	}
	var toName string
	var toActive int
	if err := s.db.QueryRow(`SELECT name,active FROM members WHERE id=?`, req.ToID).Scan(&toName, &toActive); err != nil || toActive != 1 {
		writeErr(w, 400, "pick an active member to send this to")
		return
	}
	var dup int
	s.db.QueryRow(`SELECT COUNT(*) FROM requests WHERE case_id=? AND kind=? AND from_id=? AND status='OPEN'`,
		c.ID, req.Kind, actor.ID).Scan(&dup)
	if dup > 0 {
		writeErr(w, 409, "you already have an open "+strings.ToLower(req.Kind)+" request on this case")
		return
	}
	if req.Kind == "COVERAGE" {
		var active int
		s.db.QueryRow(`SELECT COUNT(*) FROM coverage WHERE case_id=? AND coverer_id=? AND status='ACTIVE'`, c.ID, req.ToID).Scan(&active)
		if active > 0 {
			writeErr(w, 409, toName+" is already covering this case")
			return
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO requests(case_id,kind,from_id,to_id,message,status,created_at) VALUES(?,?,?,?,?,'OPEN',?)`,
		c.ID, req.Kind, actor.ID, req.ToID, req.Message, nowStamp()); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := addEvent(tx, c.ID, actor.ID, "REQUEST", map[string]any{
		"kind": req.Kind, "to": toName, "message": req.Message,
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "requests": s.openRequestsForCase(c.ID)})
}

func (s *server) handleResolveRequest(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Decision string `json:"decision"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	req.Decision = strings.ToUpper(strings.TrimSpace(req.Decision))
	if req.Decision != "ACCEPTED" && req.Decision != "DECLINED" {
		writeErr(w, 400, "decision must be accepted or declined")
		return
	}
	var caseID, fromID, toID int64
	var kind, status string
	err = s.db.QueryRow(`SELECT case_id,kind,from_id,to_id,status FROM requests WHERE id=?`, id).
		Scan(&caseID, &kind, &fromID, &toID, &status)
	if err != nil {
		writeErr(w, 404, "request not found")
		return
	}
	if status != "OPEN" {
		writeErr(w, 409, "this request was already "+strings.ToLower(status))
		return
	}
	if toID != actor.ID {
		writeErr(w, 403, "this request is not addressed to you")
		return
	}
	var c caseRow
	if c, err = scanCase(s.db.QueryRow(caseSelect+` WHERE c.id=?`, caseID)); err != nil {
		writeErr(w, 404, "case not found")
		return
	}
	var fromName string
	s.db.QueryRow(`SELECT name FROM members WHERE id=?`, fromID).Scan(&fromName)

	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE requests SET status=?, resolved_at=?, resolved_by=? WHERE id=? AND status='OPEN'`,
		req.Decision, nowStamp(), actor.ID, id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 409, "someone resolved this request first — refresh")
		return
	}
	if err := addEvent(tx, caseID, actor.ID, "DECISION", map[string]any{
		"kind": kind, "from": fromName, "decision": req.Decision,
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if req.Decision == "ACCEPTED" {
		switch kind {
		case "COVERAGE":
			// the owner asked actor to cover; actor accepting becomes the coverer
			if c.OwnerID != fromID {
				writeErr(w, 409, "ownership changed since this request was sent — ask again")
				return
			}
			if _, err := tx.Exec(`INSERT INTO coverage(case_id,owner_id,coverer_id,start_date,status,created_at) VALUES(?,?,?,?,'ACTIVE',?)`,
				caseID, c.OwnerID, actor.ID, today(), nowStamp()); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			if err := addEvent(tx, caseID, actor.ID, "COVERAGE", map[string]any{
				"action": "started", "coverer": actor.Name, "owner": c.OwnerName,
			}); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
		case "TRANSFER":
			// actor is the current owner approving a hand-over to the requester
			if c.OwnerID != actor.ID {
				writeErr(w, 409, "you are no longer the owner of this case")
				return
			}
			if _, err := tx.Exec(`UPDATE cases SET owner_id=?, updated_at=? WHERE id=?`, fromID, nowStamp(), caseID); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			if _, err := tx.Exec(`UPDATE coverage SET status='ENDED', end_date=? WHERE case_id=? AND status='ACTIVE'`, today(), caseID); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			if err := addEvent(tx, caseID, actor.ID, "ASSIGNED", map[string]any{
				"to": fromName, "transfer": true,
			}); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *server) handleWithdrawRequest(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var caseID, fromID int64
	var kind, status string
	if err := s.db.QueryRow(`SELECT case_id,kind,from_id,status FROM requests WHERE id=?`, id).
		Scan(&caseID, &kind, &fromID, &status); err != nil {
		writeErr(w, 404, "request not found")
		return
	}
	if fromID != actor.ID {
		writeErr(w, 403, "only the sender can withdraw a request")
		return
	}
	if status != "OPEN" {
		writeErr(w, 409, "this request was already "+strings.ToLower(status))
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE requests SET status='WITHDRAWN', resolved_at=?, resolved_by=? WHERE id=?`,
		nowStamp(), actor.ID, id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := addEvent(tx, caseID, actor.ID, "REQUEST", map[string]any{"kind": kind, "withdrawn": true}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *server) handleInbox(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	get := func(q string, args ...any) []requestRow {
		rows, err := s.db.Query(q, args...)
		if err != nil {
			return []requestRow{}
		}
		defer rows.Close()
		return scanRequests(rows)
	}
	writeJSON(w, 200, map[string]any{
		"incoming": get(reqSelect+` WHERE r.to_id=? AND r.status='OPEN' ORDER BY r.id DESC`, actor.ID),
		"outgoing": get(reqSelect+` WHERE r.from_id=? AND r.status='OPEN' ORDER BY r.id DESC`, actor.ID),
		"recent": get(reqSelect+` WHERE (r.to_id=? OR r.from_id=?) AND r.status!='OPEN' ORDER BY r.resolved_at DESC LIMIT 20`,
			actor.ID, actor.ID),
	})
}

func (s *server) handleInboxCount(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeJSON(w, 200, map[string]any{"count": 0})
		return
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM requests WHERE to_id=? AND status='OPEN'`, actor.ID).Scan(&n)
	writeJSON(w, 200, map[string]any{"count": n})
}

func (s *server) handleEndCoverage(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	c, err := s.caseByNo(r.PathValue("no"))
	if err != nil {
		writeErr(w, 404, "case not found")
		return
	}
	if c.CovererID == 0 {
		writeErr(w, 409, "no active coverage on this case")
		return
	}
	if actor.ID != c.OwnerID && actor.ID != c.CovererID {
		writeErr(w, 403, "only the owner or the coverer can end coverage")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE coverage SET status='ENDED', end_date=? WHERE case_id=? AND status='ACTIVE'`, today(), c.ID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := addEvent(tx, c.ID, actor.ID, "COVERAGE", map[string]any{
		"action": "ended", "coverer": c.CovererName,
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	c2, _ := s.caseByNo(c.CaseNo)
	writeJSON(w, 200, map[string]any{"case": c2})
}
