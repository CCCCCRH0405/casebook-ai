package main

import (
	"net/http"
	"strconv"
	"strings"
)

type checkItem struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
	State string `json:"state"`
}

var checkStates = map[string]bool{"Needed": true, "Requested": true, "Received": true, "N/A": true}

func (s *server) checklistFor(caseID int64) []checkItem {
	rows, err := s.db.Query(`SELECT id,label,state FROM checklist_items WHERE case_id=? ORDER BY sort,id`, caseID)
	if err != nil {
		return []checkItem{}
	}
	defer rows.Close()
	out := []checkItem{}
	for rows.Next() {
		var it checkItem
		if rows.Scan(&it.ID, &it.Label, &it.State) == nil {
			out = append(out, it)
		}
	}
	return out
}

func (s *server) checklistCaseGuard(w http.ResponseWriter, r *http.Request, caseNo string) (caseRow, member, bool) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return caseRow{}, member{}, false
	}
	c, err := s.caseByNo(caseNo)
	if err != nil {
		writeErr(w, 404, "case not found")
		return caseRow{}, member{}, false
	}
	if !s.canEdit(c, actor) {
		writeErr(w, 403, "only the owner (or an active coverer) can edit the checklist")
		return caseRow{}, member{}, false
	}
	if c.Status == "Archived" {
		writeErr(w, 409, "archived cases are read-only — reopen first")
		return caseRow{}, member{}, false
	}
	return c, actor, true
}

func (s *server) handleAddChecklistItem(w http.ResponseWriter, r *http.Request) {
	c, actor, ok := s.checklistCaseGuard(w, r, r.PathValue("no"))
	if !ok {
		return
	}
	var req struct {
		Label string `json:"label"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	if req.Label == "" {
		writeErr(w, 400, "label is required")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO checklist_items(case_id,label,state,sort,created_at)
VALUES(?,?,'Needed',(SELECT COALESCE(MAX(sort),0)+1 FROM checklist_items WHERE case_id=?),?)`,
		c.ID, req.Label, c.ID, nowStamp()); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := addEvent(tx, c.ID, actor.ID, "CHECKLIST", map[string]any{"label": req.Label, "action": "added"}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"checklist": s.checklistFor(c.ID)})
}

func (s *server) checklistItemCase(itemID int64) (int64, string, string, error) {
	var caseID int64
	var label, caseNo string
	err := s.db.QueryRow(`SELECT ci.case_id, ci.label, c.case_no FROM checklist_items ci JOIN cases c ON c.id=ci.case_id WHERE ci.id=?`, itemID).
		Scan(&caseID, &label, &caseNo)
	return caseID, label, caseNo, err
}

func (s *server) handlePatchChecklistItem(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_, label, caseNo, err := s.checklistItemCase(id)
	if err != nil {
		writeErr(w, 404, "checklist item not found")
		return
	}
	c, actor, ok := s.checklistCaseGuard(w, r, caseNo)
	if !ok {
		return
	}
	var req struct {
		State string `json:"state"`
	}
	if err := readJSON(r, &req); err != nil || !checkStates[req.State] {
		writeErr(w, 400, "state must be one of: Needed, Requested, Received, N/A")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE checklist_items SET state=? WHERE id=?`, req.State, id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := addEvent(tx, c.ID, actor.ID, "CHECKLIST", map[string]any{"label": label, "state": req.State}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"checklist": s.checklistFor(c.ID)})
}

func (s *server) handleDeleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_, label, caseNo, err := s.checklistItemCase(id)
	if err != nil {
		writeErr(w, 404, "checklist item not found")
		return
	}
	c, actor, ok := s.checklistCaseGuard(w, r, caseNo)
	if !ok {
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM checklist_items WHERE id=?`, id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := addEvent(tx, c.ID, actor.ID, "CHECKLIST", map[string]any{"label": label, "action": "removed"}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"checklist": s.checklistFor(c.ID)})
}
