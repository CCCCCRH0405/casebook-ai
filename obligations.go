package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

var frequencies = map[string]bool{"Yearly": true, "Quarterly": true, "Monthly": true, "Weekly": true}

func addPeriod(d, freq string) string {
	t, err := parseDate(d)
	if err != nil {
		return ""
	}
	switch freq {
	case "Yearly":
		t = t.AddDate(1, 0, 0)
	case "Quarterly":
		t = t.AddDate(0, 3, 0)
	case "Monthly":
		t = t.AddDate(0, 1, 0)
	case "Weekly":
		t = t.AddDate(0, 0, 7)
	default:
		return ""
	}
	return t.Format("2006-01-02")
}

func occurrenceLabel(d, freq string) string {
	t, err := parseDate(d)
	if err != nil {
		return d
	}
	switch freq {
	case "Yearly":
		return fmt.Sprintf("%d", t.Year())
	case "Quarterly":
		return fmt.Sprintf("Q%d %d", (int(t.Month())-1)/3+1, t.Year())
	case "Monthly":
		return t.Format("Jan 2006")
	default:
		return "week of " + t.Format("Jan 2")
	}
}

type obligationRow struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	TypeID    int64    `json:"type_id"`
	TypeName  string   `json:"type_name"`
	OwnerID   int64    `json:"owner_id"`
	OwnerName string   `json:"owner_name"`
	Frequency string   `json:"frequency"`
	NextDue   string   `json:"next_due"`
	LeadDays  int      `json:"lead_days"`
	Checklist []string `json:"checklist"`
	Active    bool     `json:"active"`
	CaseCount int      `json:"case_count"`
}

func (s *server) listObligations() []obligationRow {
	rows, err := s.db.Query(`SELECT o.id,o.name,COALESCE(o.case_type_id,0),COALESCE(t.name,''),
COALESCE(o.default_owner_id,0),COALESCE(m.name,''),o.frequency,o.next_due,o.lead_days,o.checklist,o.active,
(SELECT COUNT(*) FROM cases c WHERE c.obligation_id=o.id)
FROM obligations o LEFT JOIN case_types t ON t.id=o.case_type_id LEFT JOIN members m ON m.id=o.default_owner_id
ORDER BY o.active DESC, o.next_due`)
	if err != nil {
		return []obligationRow{}
	}
	defer rows.Close()
	out := []obligationRow{}
	for rows.Next() {
		var o obligationRow
		var active int
		var chk string
		if rows.Scan(&o.ID, &o.Name, &o.TypeID, &o.TypeName, &o.OwnerID, &o.OwnerName,
			&o.Frequency, &o.NextDue, &o.LeadDays, &chk, &active, &o.CaseCount) == nil {
			o.Active = active == 1
			o.Checklist = []string{}
			json.Unmarshal([]byte(chk), &o.Checklist)
			out = append(out, o)
		}
	}
	return out
}

func cleanChecklist(items []string) []string {
	out := []string{}
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it != "" {
			out = append(out, it)
		}
	}
	return out
}

func (s *server) handleListObligations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"obligations": s.listObligations()})
}

func (s *server) handleCreateObligation(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	var req struct {
		Name      string   `json:"name"`
		TypeID    int64    `json:"type_id"`
		OwnerID   int64    `json:"owner_id"`
		Frequency string   `json:"frequency"`
		NextDue   string   `json:"next_due"`
		LeadDays  int      `json:"lead_days"`
		Checklist []string `json:"checklist"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeErr(w, 400, "name is required")
		return
	}
	if !frequencies[req.Frequency] {
		writeErr(w, 400, "frequency must be Yearly, Quarterly, Monthly or Weekly")
		return
	}
	if _, err := parseDate(req.NextDue); err != nil {
		writeErr(w, 400, "next due date is required (the first deadline this should generate a case for)")
		return
	}
	if req.LeadDays < 0 || req.LeadDays > 365 {
		writeErr(w, 400, "lead days must be between 0 and 365")
		return
	}
	chk, _ := json.Marshal(cleanChecklist(req.Checklist))
	var typeID, ownerID any
	if req.TypeID > 0 {
		typeID = req.TypeID
	}
	if req.OwnerID > 0 {
		ownerID = req.OwnerID
	}
	if _, err := s.db.Exec(`INSERT INTO obligations(name,case_type_id,default_owner_id,frequency,next_due,lead_days,checklist,active,created_at,created_by)
VALUES(?,?,?,?,?,?,?,1,?,?)`,
		req.Name, typeID, ownerID, req.Frequency, req.NextDue, req.LeadDays, string(chk), nowStamp(), actor.ID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"obligations": s.listObligations()})
}

func (s *server) handlePatchObligation(w http.ResponseWriter, r *http.Request) {
	if _, err := s.actor(r); err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Active *bool `json:"active"`
	}
	if err := readJSON(r, &req); err != nil || req.Active == nil {
		writeErr(w, 400, "bad request body")
		return
	}
	v := 0
	if *req.Active {
		v = 1
	}
	if _, err := s.db.Exec(`UPDATE obligations SET active=? WHERE id=?`, v, id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"obligations": s.listObligations()})
}

// spawnNext creates the case for an obligation's next occurrence and advances next_due.
// If a case already exists for that occurrence (same obligation and due date) it only
// advances next_due and reports created=false — the spec's "one case per occurrence" invariant.
func spawnNext(db *sql.DB, obligationID int64, actorID int64) (no string, created bool, err error) {
	tx, err := db.Begin()
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var name, freq, nextDue, chkJSON string
	var typeID, ownerID sql.NullInt64
	var active int
	err = tx.QueryRow(`SELECT name,case_type_id,default_owner_id,frequency,next_due,checklist,active FROM obligations WHERE id=?`, obligationID).
		Scan(&name, &typeID, &ownerID, &freq, &nextDue, &chkJSON, &active)
	if err != nil {
		return "", false, fmt.Errorf("recurring item not found")
	}
	if active != 1 {
		return "", false, fmt.Errorf("this recurring item is paused")
	}
	if _, err := parseDate(nextDue); err != nil {
		return "", false, fmt.Errorf("recurring item has no valid next due date")
	}
	var dup int
	tx.QueryRow(`SELECT COUNT(*) FROM cases WHERE obligation_id=? AND due_date=?`, obligationID, nextDue).Scan(&dup)
	if dup > 0 {
		if _, err := tx.Exec(`UPDATE obligations SET next_due=? WHERE id=?`, addPeriod(nextDue, freq), obligationID); err != nil {
			return "", false, err
		}
		if err := tx.Commit(); err != nil {
			return "", false, err
		}
		return "", false, nil
	}
	no, err = nextCaseNo(tx)
	if err != nil {
		return "", false, err
	}
	label := occurrenceLabel(nextDue, freq)
	title := name + " — " + label
	var typeArg, ownerArg, actorArg any
	if typeID.Valid {
		typeArg = typeID.Int64
	}
	if ownerID.Valid {
		ownerArg = ownerID.Int64
	}
	if actorID > 0 {
		actorArg = actorID
	}
	res, err := tx.Exec(`INSERT INTO cases(case_no,title,case_type_id,status,owner_id,due_date,created_at,created_by,updated_at,obligation_id)
VALUES(?,?,?,'Open',?,?,?,?,?,?)`,
		no, title, typeArg, ownerArg, nextDue, nowStamp(), actorArg, nowStamp(), obligationID)
	if err != nil {
		return "", false, err
	}
	caseID, _ := res.LastInsertId()
	if err := addEvent(tx, caseID, actorID, "SPAWNED", map[string]any{
		"obligation": name, "occurrence": label, "auto": actorID == 0,
	}); err != nil {
		return "", false, err
	}
	var labels []string
	json.Unmarshal([]byte(chkJSON), &labels)
	for i, l := range labels {
		if _, err := tx.Exec(`INSERT INTO checklist_items(case_id,label,state,sort,created_at) VALUES(?,?,'Needed',?,?)`,
			caseID, l, i+1, nowStamp()); err != nil {
			return "", false, err
		}
	}
	next := addPeriod(nextDue, freq)
	if _, err := tx.Exec(`UPDATE obligations SET next_due=? WHERE id=?`, next, obligationID); err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return no, true, nil
}

func (s *server) handleSpawnObligation(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	no, created, err := spawnNext(s.db, id, actor.ID)
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	if !created {
		writeErr(w, 409, "that occurrence already has a case — the schedule moved to the next one")
		return
	}
	writeJSON(w, 200, map[string]any{"case_no": no, "obligations": s.listObligations()})
}

func (s *server) handleMakeRecurring(w http.ResponseWriter, r *http.Request) {
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
	if !s.canEdit(c, actor) {
		writeErr(w, 403, "only the owner (or an active coverer) can make a case recurring")
		return
	}
	if c.ObligationID != 0 {
		writeErr(w, 409, "this case already belongs to a recurring item")
		return
	}
	var req struct {
		Frequency string `json:"frequency"`
		NextDue   string `json:"next_due"`
		LeadDays  int    `json:"lead_days"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	if !frequencies[req.Frequency] {
		writeErr(w, 400, "frequency must be Yearly, Quarterly, Monthly or Weekly")
		return
	}
	if _, err := parseDate(req.NextDue); err != nil {
		writeErr(w, 400, "pick the next due date (when the next occurrence is due)")
		return
	}
	if req.LeadDays < 0 || req.LeadDays > 365 {
		writeErr(w, 400, "lead days must be between 0 and 365")
		return
	}
	labels := []string{}
	for _, it := range s.checklistFor(c.ID) {
		labels = append(labels, it.Label)
	}
	chk, _ := json.Marshal(labels)
	var typeArg, ownerArg any
	if c.TypeID > 0 {
		typeArg = c.TypeID
	}
	if c.OwnerID > 0 {
		ownerArg = c.OwnerID
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO obligations(name,case_type_id,default_owner_id,frequency,next_due,lead_days,checklist,active,created_at,created_by)
VALUES(?,?,?,?,?,?,?,1,?,?)`,
		c.Title, typeArg, ownerArg, req.Frequency, req.NextDue, req.LeadDays, string(chk), nowStamp(), actor.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	obID, _ := res.LastInsertId()
	if _, err := tx.Exec(`UPDATE cases SET obligation_id=?, updated_at=? WHERE id=?`, obID, nowStamp(), c.ID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := addEvent(tx, c.ID, actor.ID, "RECURRING", map[string]any{
		"frequency": req.Frequency, "next_due": req.NextDue,
		"checklist_items": len(labels),
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

// runSpawner creates cases for every active obligation whose lead window has opened.
func runSpawner(db *sql.DB) {
	rows, err := db.Query(`SELECT id FROM obligations WHERE active=1`)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	td := today()
	const catchUpCap = 24
	for _, id := range ids {
		i := 0
		for ; i < catchUpCap; i++ {
			var nextDue string
			var lead int
			if err := db.QueryRow(`SELECT next_due,lead_days FROM obligations WHERE id=? AND active=1`, id).
				Scan(&nextDue, &lead); err != nil {
				break
			}
			t, err := parseDate(nextDue)
			if err != nil {
				break
			}
			spawnDate := t.AddDate(0, 0, -lead).Format("2006-01-02")
			if td < spawnDate {
				break
			}
			if _, _, err := spawnNext(db, id, 0); err != nil {
				log.Printf("spawner: obligation %d: %v", id, err)
				break
			}
		}
		if i == catchUpCap {
			log.Printf("spawner: obligation %d hit the catch-up cap of %d occurrences in one pass; will continue next pass", id, catchUpCap)
		}
	}
}
