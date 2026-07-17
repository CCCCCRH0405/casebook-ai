package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type server struct {
	db *sql.DB
	ws string
}

var transitions = map[string][]string{
	"Open":        {"In Progress", "Waiting", "Completed", "Cancelled"},
	"In Progress": {"Open", "Waiting", "Completed", "Cancelled"},
	"Waiting":     {"Open", "In Progress", "Completed", "Cancelled"},
	"Completed":   {"In Progress", "Archived"},
	"Archived":    {"In Progress"},
	"Cancelled":   {"Open"},
}

func canTransition(from, to string) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

func isClosed(status string) bool {
	return status == "Completed" || status == "Archived" || status == "Cancelled"
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

type member struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Initials string `json:"initials"`
	Active   bool   `json:"active"`
	IsAdmin  bool   `json:"is_admin"`
	HasPin   bool   `json:"has_pin"`
}

func hashPin(memberID int64, pin string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("cb:%d:%s", memberID, pin)))
	return hex.EncodeToString(sum[:])
}

func (s *server) memberWithPin(id int64) (member, string, error) {
	var m member
	var active, admin int
	var pinHash string
	err := s.db.QueryRow(`SELECT id,name,initials,active,is_admin,pin_hash FROM members WHERE id=?`, id).
		Scan(&m.ID, &m.Name, &m.Initials, &active, &admin, &pinHash)
	if err != nil {
		return member{}, "", errors.New("unknown member")
	}
	m.Active, m.IsAdmin, m.HasPin = active == 1, admin == 1, pinHash != ""
	return m, pinHash, nil
}

func (s *server) actor(r *http.Request) (member, error) {
	idStr := r.Header.Get("X-Member")
	pin := r.Header.Get("X-Pin")
	if idStr == "" && r.Method == http.MethodGet {
		// browser downloads (links) cannot send headers; allow query-param identity
		// for GET only so a PIN never rides a mutation URL into logs or history
		idStr = r.URL.Query().Get("member")
		pin = r.URL.Query().Get("pin")
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return member{}, errors.New("missing identity: pick who you are first")
	}
	m, pinHash, err := s.memberWithPin(id)
	if err != nil {
		return member{}, err
	}
	if !m.Active {
		return member{}, errors.New("this member is deactivated")
	}
	if pinHash != "" && hashPin(id, pin) != pinHash {
		return member{}, errors.New("wrong PIN — switch identity and sign in again")
	}
	return m, nil
}

func initialsOf(name string) string {
	parts := strings.Fields(name)
	out := ""
	for _, p := range parts {
		r := []rune(p)
		if len(r) > 0 {
			out += strings.ToUpper(string(r[0]))
		}
		if len(out) >= 2 {
			break
		}
	}
	if out == "" {
		out = "?"
	}
	return out
}

// ---------- bootstrap & setup ----------

func (s *server) listMembers() []member {
	rows, _ := s.db.Query(`SELECT id,name,initials,active,is_admin,pin_hash!='' FROM members ORDER BY id`)
	defer rows.Close()
	out := []member{}
	for rows.Next() {
		var m member
		var active, admin, hasPin int
		if rows.Scan(&m.ID, &m.Name, &m.Initials, &active, &admin, &hasPin) == nil {
			m.Active, m.IsAdmin, m.HasPin = active == 1, admin == 1, hasPin == 1
			out = append(out, m)
		}
	}
	return out
}

var pinFormat = regexp.MustCompile(`^\d{4,8}$`)

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MemberID int64  `json:"member_id"`
		Pin      string `json:"pin"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	m, pinHash, err := s.memberWithPin(req.MemberID)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	if !m.Active {
		writeErr(w, 403, "this member is deactivated")
		return
	}
	if pinHash != "" && hashPin(m.ID, req.Pin) != pinHash {
		writeErr(w, 401, "wrong PIN")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "member": m})
}

func (s *server) handleSetPin(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Pin    string `json:"pin"`
		OldPin string `json:"old_pin"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	_, targetHash, err := s.memberWithPin(id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	if actor.ID == id {
		if targetHash != "" && hashPin(id, req.OldPin) != targetHash {
			writeErr(w, 403, "current PIN is wrong")
			return
		}
		if req.Pin == "" {
			s.db.Exec(`UPDATE members SET pin_hash='' WHERE id=?`, id)
			writeJSON(w, 200, map[string]any{"ok": true, "members": s.listMembers()})
			return
		}
		if !pinFormat.MatchString(req.Pin) {
			writeErr(w, 400, "PIN must be 4–8 digits")
			return
		}
		s.db.Exec(`UPDATE members SET pin_hash=? WHERE id=?`, hashPin(id, req.Pin), id)
		writeJSON(w, 200, map[string]any{"ok": true, "members": s.listMembers()})
		return
	}
	if actor.IsAdmin && req.Pin == "" {
		// rescue path: the workspace admin can clear a forgotten PIN, never set one
		s.db.Exec(`UPDATE members SET pin_hash='' WHERE id=?`, id)
		writeJSON(w, 200, map[string]any{"ok": true, "members": s.listMembers()})
		return
	}
	writeErr(w, 403, "you can only manage your own PIN")
}

type caseType struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

func (s *server) listTypes() []caseType {
	rows, _ := s.db.Query(`SELECT id,name,active FROM case_types ORDER BY sort,id`)
	defer rows.Close()
	out := []caseType{}
	for rows.Next() {
		var t caseType
		var active int
		if rows.Scan(&t.ID, &t.Name, &active) == nil {
			t.Active = active == 1
			out = append(out, t)
		}
	}
	return out
}

func (s *server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"setup_done":      getMeta(s.db, "setup_done") == "1",
		"team_name":       getMeta(s.db, "team_name"),
		"members":         s.listMembers(),
		"types":           s.listTypes(),
		"waiting_options": waitingOptions,
		"transitions":     transitions,
		"version":         appVersion,
	})
}

func (s *server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if getMeta(s.db, "setup_done") == "1" {
		writeErr(w, 409, "setup already completed")
		return
	}
	var req struct {
		Team    string   `json:"team"`
		You     string   `json:"you"`
		Members []string `json:"members"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	req.Team = strings.TrimSpace(req.Team)
	req.You = strings.TrimSpace(req.You)
	if req.Team == "" || req.You == "" {
		writeErr(w, 400, "team name and your name are required")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO members(name,initials,active,is_admin,created_at) VALUES(?,?,1,1,?)`,
		req.You, initialsOf(req.You), nowStamp())
	if err != nil {
		writeErr(w, 400, "could not create member: "+err.Error())
		return
	}
	youID, _ := res.LastInsertId()
	for _, name := range req.Members {
		name = strings.TrimSpace(name)
		if name == "" || name == req.You {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO members(name,initials,active,is_admin,created_at) VALUES(?,?,1,0,?)`,
			name, initialsOf(name), nowStamp()); err != nil {
			writeErr(w, 400, "duplicate member name: "+name)
			return
		}
	}
	if err := setMeta(tx, "team_name", req.Team); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := setMeta(tx, "setup_done", "1"); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "you": youID})
}

// ---------- members & types ----------

func (s *server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	if _, err := s.actor(r); err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	var req struct {
		Name string `json:"name"`
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
	_, err := s.db.Exec(`INSERT INTO members(name,initials,active,is_admin,created_at) VALUES(?,?,1,0,?)`,
		req.Name, initialsOf(req.Name), nowStamp())
	if err != nil {
		writeErr(w, 400, "a member with that name already exists")
		return
	}
	writeJSON(w, 200, map[string]any{"members": s.listMembers()})
}

func (s *server) handlePatchMember(w http.ResponseWriter, r *http.Request) {
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
	if v == 0 {
		var isAdmin, otherActiveAdmins int
		s.db.QueryRow(`SELECT is_admin FROM members WHERE id=?`, id).Scan(&isAdmin)
		s.db.QueryRow(`SELECT COUNT(*) FROM members WHERE is_admin=1 AND active=1 AND id!=?`, id).Scan(&otherActiveAdmins)
		if isAdmin == 1 && otherActiveAdmins == 0 {
			writeErr(w, 400, "cannot deactivate the only workspace admin")
			return
		}
	}
	if _, err := s.db.Exec(`UPDATE members SET active=? WHERE id=?`, v, id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"members": s.listMembers()})
}

func (s *server) handleAddType(w http.ResponseWriter, r *http.Request) {
	if _, err := s.actor(r); err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	var req struct {
		Name string `json:"name"`
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
	if _, err := s.db.Exec(`INSERT INTO case_types(name,sort) VALUES(?, (SELECT COALESCE(MAX(sort),0)+1 FROM case_types))`, req.Name); err != nil {
		writeErr(w, 400, "a type with that name already exists")
		return
	}
	writeJSON(w, 200, map[string]any{"types": s.listTypes()})
}

// ---------- cases ----------

type caseRow struct {
	ID            int64  `json:"id"`
	CaseNo        string `json:"case_no"`
	Title         string `json:"title"`
	TypeID        int64  `json:"type_id"`
	TypeName      string `json:"type_name"`
	Status        string `json:"status"`
	OwnerID       int64  `json:"owner_id"`
	OwnerName     string `json:"owner_name"`
	DueDate       string `json:"due_date"`
	Deadline      string `json:"deadline"`
	WaitingOn     string `json:"waiting_on"`
	WaitingDetail string `json:"waiting_detail"`
	WaitingSince  string `json:"waiting_since"`
	Source        string `json:"source"`
	Location      string `json:"location"`
	Tags          string `json:"tags"`
	Description   string `json:"description"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	CompletedAt   string `json:"completed_at"`
	CovererID     int64  `json:"coverer_id"`
	CovererName   string `json:"coverer_name"`
	ObligationID  int64  `json:"obligation_id"`
}

const caseSelect = `SELECT c.id,c.case_no,c.title,COALESCE(c.case_type_id,0),COALESCE(t.name,''),c.status,
COALESCE(c.owner_id,0),COALESCE(m.name,''),c.due_date,c.deadline,c.waiting_on,c.waiting_detail,c.waiting_since,
c.source,c.location,c.tags,c.description,c.created_at,c.updated_at,c.completed_at,
COALESCE((SELECT cv.coverer_id FROM coverage cv WHERE cv.case_id=c.id AND cv.status='ACTIVE' ORDER BY cv.id DESC LIMIT 1),0),
COALESCE((SELECT m2.name FROM coverage cv JOIN members m2 ON m2.id=cv.coverer_id WHERE cv.case_id=c.id AND cv.status='ACTIVE' ORDER BY cv.id DESC LIMIT 1),''),
COALESCE(c.obligation_id,0)
FROM cases c LEFT JOIN case_types t ON t.id=c.case_type_id LEFT JOIN members m ON m.id=c.owner_id`

func scanCase(sc interface{ Scan(...any) error }) (caseRow, error) {
	var c caseRow
	err := sc.Scan(&c.ID, &c.CaseNo, &c.Title, &c.TypeID, &c.TypeName, &c.Status, &c.OwnerID, &c.OwnerName,
		&c.DueDate, &c.Deadline, &c.WaitingOn, &c.WaitingDetail, &c.WaitingSince,
		&c.Source, &c.Location, &c.Tags, &c.Description, &c.CreatedAt, &c.UpdatedAt, &c.CompletedAt,
		&c.CovererID, &c.CovererName, &c.ObligationID)
	return c, err
}

func (s *server) caseByNo(no string) (caseRow, error) {
	return scanCase(s.db.QueryRow(caseSelect+` WHERE c.case_no=?`, no))
}

func (s *server) handleListCases(w http.ResponseWriter, r *http.Request) {
	if _, err := s.actor(r); err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	q := r.URL.Query()
	where := []string{"1=1"}
	args := []any{}
	switch q.Get("view") {
	case "archived":
		where = append(where, "c.status='Archived'")
	case "all":
	default:
		where = append(where, "c.status NOT IN ('Archived','Cancelled')")
	}
	if v := q.Get("status"); v != "" {
		where = append(where, "c.status=?")
		args = append(args, v)
	}
	if v := q.Get("owner"); v != "" {
		if v == "unassigned" {
			where = append(where, "c.owner_id IS NULL")
		} else {
			where = append(where, "c.owner_id=?")
			args = append(args, v)
		}
	}
	if v := q.Get("type"); v != "" {
		where = append(where, "c.case_type_id=?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		where = append(where, "(c.title LIKE ? OR c.case_no LIKE ? OR c.tags LIKE ? OR c.source LIKE ?)")
		pat := "%" + v + "%"
		args = append(args, pat, pat, pat, pat)
	}
	sqlStr := caseSelect + ` WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY CASE WHEN c.due_date='' THEN 1 ELSE 0 END, c.due_date, c.case_no DESC LIMIT 500`
	rows, err := s.db.Query(sqlStr, args...)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []caseRow{}
	for rows.Next() {
		if c, err := scanCase(rows); err == nil {
			out = append(out, c)
		}
	}
	writeJSON(w, 200, map[string]any{"cases": out})
}

func (s *server) handleCreateCase(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	var req struct {
		Title       string `json:"title"`
		TypeID      int64  `json:"type_id"`
		OwnerID     int64  `json:"owner_id"`
		DueDate     string `json:"due_date"`
		Deadline    string `json:"deadline"`
		Source      string `json:"source"`
		Location    string `json:"location"`
		Tags        string `json:"tags"`
		Description string `json:"description"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeErr(w, 400, "title is required")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	no, err := nextCaseNo(tx)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var typeID, ownerID any
	if req.TypeID > 0 {
		typeID = req.TypeID
	}
	if req.OwnerID > 0 {
		ownerID = req.OwnerID
	}
	res, err := tx.Exec(`INSERT INTO cases(case_no,title,case_type_id,status,owner_id,due_date,deadline,source,location,tags,description,created_at,created_by,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		no, req.Title, typeID, "Open", ownerID, req.DueDate, req.Deadline, req.Source, req.Location, req.Tags, req.Description,
		nowStamp(), actor.ID, nowStamp())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	caseID, _ := res.LastInsertId()
	if err := addEvent(tx, caseID, actor.ID, "CREATED", map[string]any{"title": req.Title}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if req.OwnerID > 0 && req.OwnerID != actor.ID {
		var name string
		tx.QueryRow(`SELECT name FROM members WHERE id=?`, req.OwnerID).Scan(&name)
		if err := addEvent(tx, caseID, actor.ID, "ASSIGNED", map[string]any{"to": name}); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	c, _ := s.caseByNo(no)
	writeJSON(w, 200, map[string]any{"case": c})
}

func (s *server) handleGetCase(w http.ResponseWriter, r *http.Request) {
	if _, err := s.actor(r); err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	c, err := s.caseByNo(r.PathValue("no"))
	if err != nil {
		writeErr(w, 404, "case not found")
		return
	}
	rows, _ := s.db.Query(`SELECT e.at,COALESCE(m.name,'System'),e.kind,e.payload FROM events e
LEFT JOIN members m ON m.id=e.actor_id WHERE e.case_id=? ORDER BY e.id`, c.ID)
	defer rows.Close()
	type ev struct {
		At      string          `json:"at"`
		Actor   string          `json:"actor"`
		Kind    string          `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}
	events := []ev{}
	for rows.Next() {
		var e ev
		var p string
		if rows.Scan(&e.At, &e.Actor, &e.Kind, &p) == nil {
			e.Payload = json.RawMessage(p)
			events = append(events, e)
		}
	}
	writeJSON(w, 200, map[string]any{"case": c, "events": events,
		"requests": s.openRequestsForCase(c.ID), "checklist": s.checklistFor(c.ID)})
}

func (s *server) canEdit(c caseRow, actor member) bool {
	return c.OwnerID == 0 || c.OwnerID == actor.ID || c.CovererID == actor.ID
}

// waiting_on / waiting_detail are deliberately absent: they only change through the
// status endpoint so they can never desync from the Waiting state and its aging clock.
var editableFields = map[string]string{
	"title": "title", "type_id": "case_type_id", "due_date": "due_date", "deadline": "deadline",
	"source": "source", "location": "location", "tags": "tags", "description": "description",
	"owner_id": "owner_id",
}

func (s *server) handlePatchCase(w http.ResponseWriter, r *http.Request) {
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
		writeErr(w, 403, "only the owner (or an active coverer) can edit this case — you can still add notes or request a transfer")
		return
	}
	if c.Status == "Archived" {
		writeErr(w, 409, "archived cases are read-only — reopen first")
		return
	}
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	old := map[string]string{
		"title": c.Title, "type_id": strconv.FormatInt(c.TypeID, 10), "due_date": c.DueDate,
		"deadline": c.Deadline, "source": c.Source, "location": c.Location, "tags": c.Tags,
		"description": c.Description, "owner_id": strconv.FormatInt(c.OwnerID, 10),
	}
	sets := []string{}
	args := []any{}
	changes := []map[string]string{}
	for field := range req {
		col, ok := editableFields[field]
		if !ok {
			writeErr(w, 400, "unknown or read-only field: "+field)
			return
		}
		var newVal string
		switch v := req[field].(type) {
		case string:
			newVal = strings.TrimSpace(v)
		case float64:
			newVal = strconv.FormatInt(int64(v), 10)
		default:
			writeErr(w, 400, "bad value for "+field)
			return
		}
		if field == "title" && newVal == "" {
			writeErr(w, 400, "title cannot be empty")
			return
		}
		if newVal == old[field] {
			continue
		}
		if field == "type_id" || field == "owner_id" {
			n, _ := strconv.ParseInt(newVal, 10, 64)
			if n > 0 {
				sets = append(sets, col+"=?")
				args = append(args, n)
			} else {
				sets = append(sets, col+"=NULL")
			}
		} else {
			sets = append(sets, col+"=?")
			args = append(args, newVal)
		}
		changes = append(changes, map[string]string{"field": field, "from": old[field], "to": newVal})
	}
	if len(sets) == 0 {
		writeJSON(w, 200, map[string]any{"case": c, "unchanged": true})
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	args = append(args, nowStamp(), c.ID)
	if _, err := tx.Exec(`UPDATE cases SET `+strings.Join(sets, ",")+`, updated_at=? WHERE id=?`, args...); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	kind := "FIELD_CHANGE"
	for _, ch := range changes {
		if ch["field"] == "owner_id" {
			kind = "ASSIGNED"
		}
	}
	if err := addEvent(tx, c.ID, actor.ID, kind, map[string]any{"changes": changes}); err != nil {
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

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
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
		writeErr(w, 403, "only the owner (or an active coverer) can change status")
		return
	}
	var req struct {
		To            string `json:"to"`
		WaitingOn     string `json:"waiting_on"`
		WaitingDetail string `json:"waiting_detail"`
		Reason        string `json:"reason"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	if !canTransition(c.Status, req.To) {
		writeErr(w, 409, fmt.Sprintf("cannot move from %s to %s", c.Status, req.To))
		return
	}
	if req.To == "Waiting" && strings.TrimSpace(req.WaitingOn) == "" {
		writeErr(w, 400, "pick who you are waiting on")
		return
	}
	if req.To == "Cancelled" && strings.TrimSpace(req.Reason) == "" {
		writeErr(w, 400, "a reason is required to cancel")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	sets := []string{"status=?", "updated_at=?"}
	args := []any{req.To, nowStamp()}
	switch req.To {
	case "Waiting":
		sets = append(sets, "waiting_on=?", "waiting_detail=?", "waiting_since=?")
		args = append(args, req.WaitingOn, strings.TrimSpace(req.WaitingDetail), today())
	case "Completed":
		sets = append(sets, "completed_at=?", "waiting_on=''", "waiting_detail=''", "waiting_since=''")
		args = append(args, nowStamp())
	case "Cancelled":
		sets = append(sets, "cancelled_reason=?", "waiting_on=''", "waiting_detail=''", "waiting_since=''")
		args = append(args, strings.TrimSpace(req.Reason))
	case "Archived":
		sets = append(sets, "archived_at=?")
		args = append(args, nowStamp())
	default:
		sets = append(sets, "waiting_on=''", "waiting_detail=''", "waiting_since=''")
		if isClosed(c.Status) {
			sets = append(sets, "completed_at=''", "archived_at=''", "cancelled_reason=''")
		}
	}
	args = append(args, c.ID)
	if _, err := tx.Exec(`UPDATE cases SET `+strings.Join(sets, ",")+` WHERE id=?`, args...); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	kind := "STATUS"
	if isClosed(c.Status) && !isClosed(req.To) {
		kind = "REOPENED"
	} else if req.To == "Archived" {
		kind = "ARCHIVED"
	}
	payload := map[string]any{"from": c.Status, "to": req.To}
	if req.To == "Waiting" {
		payload["waiting_on"] = req.WaitingOn
		if req.WaitingDetail != "" {
			payload["detail"] = req.WaitingDetail
		}
	}
	if req.Reason != "" {
		payload["reason"] = req.Reason
	}
	if err := addEvent(tx, c.ID, actor.ID, kind, payload); err != nil {
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

func (s *server) handleNote(w http.ResponseWriter, r *http.Request) {
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
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "bad request body")
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		writeErr(w, 400, "note is empty")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	if err := addEvent(tx, c.ID, actor.ID, "NOTE", map[string]any{"text": req.Text}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if _, err := tx.Exec(`UPDATE cases SET updated_at=? WHERE id=?`, nowStamp(), c.ID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *server) handleClaim(w http.ResponseWriter, r *http.Request) {
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
	if c.OwnerID != 0 {
		writeErr(w, 409, "already owned by "+c.OwnerName)
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE cases SET owner_id=?, updated_at=? WHERE id=? AND owner_id IS NULL`, actor.ID, nowStamp(), c.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 409, "someone claimed it first — refresh")
		return
	}
	if err := addEvent(tx, c.ID, actor.ID, "ASSIGNED", map[string]any{"to": actor.Name, "claim": true}); err != nil {
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

func (s *server) handleToday(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	rows, err := s.db.Query(caseSelect+` WHERE c.status NOT IN ('Completed','Archived','Cancelled')
AND (c.owner_id=? OR c.id IN (SELECT case_id FROM coverage WHERE coverer_id=? AND status='ACTIVE'))
ORDER BY CASE WHEN c.due_date='' THEN 1 ELSE 0 END, c.due_date`, actor.ID, actor.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer rows.Close()
	td := today()
	week := dateAdd(td, 7)
	var overdue, dueToday, dueWeek, waiting, rest, covering []caseRow
	for rows.Next() {
		c, err := scanCase(rows)
		if err != nil {
			continue
		}
		switch {
		case c.OwnerID != actor.ID:
			covering = append(covering, c)
		case c.Status == "Waiting":
			waiting = append(waiting, c)
		case c.DueDate != "" && c.DueDate < td:
			overdue = append(overdue, c)
		case c.DueDate == td:
			dueToday = append(dueToday, c)
		case c.DueDate != "" && c.DueDate <= week:
			dueWeek = append(dueWeek, c)
		default:
			rest = append(rest, c)
		}
	}
	var unassigned, inboxCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM cases WHERE owner_id IS NULL AND status NOT IN ('Completed','Archived','Cancelled')`).Scan(&unassigned)
	s.db.QueryRow(`SELECT COUNT(*) FROM requests WHERE to_id=? AND status='OPEN'`, actor.ID).Scan(&inboxCount)
	writeJSON(w, 200, map[string]any{
		"overdue": nz(overdue), "due_today": nz(dueToday), "due_week": nz(dueWeek),
		"waiting": nz(waiting), "rest": nz(rest), "covering": nz(covering),
		"unassigned_count": unassigned, "inbox_count": inboxCount,
	})
}

func nz(v []caseRow) []caseRow {
	if v == nil {
		return []caseRow{}
	}
	return v
}

func dateAdd(d string, days int) string {
	t, err := parseDate(d)
	if err != nil {
		return d
	}
	return t.AddDate(0, 0, days).Format("2006-01-02")
}

func (s *server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/members", s.handleAddMember)
	mux.HandleFunc("PATCH /api/members/{id}", s.handlePatchMember)
	mux.HandleFunc("POST /api/types", s.handleAddType)
	mux.HandleFunc("GET /api/cases", s.handleListCases)
	mux.HandleFunc("POST /api/cases", s.handleCreateCase)
	mux.HandleFunc("GET /api/cases/{no}", s.handleGetCase)
	mux.HandleFunc("PATCH /api/cases/{no}", s.handlePatchCase)
	mux.HandleFunc("POST /api/cases/{no}/status", s.handleStatus)
	mux.HandleFunc("POST /api/cases/{no}/notes", s.handleNote)
	mux.HandleFunc("POST /api/cases/{no}/claim", s.handleClaim)
	mux.HandleFunc("GET /api/today", s.handleToday)
	mux.HandleFunc("POST /api/cases/{no}/requests", s.handleCreateRequest)
	mux.HandleFunc("POST /api/cases/{no}/coverage/end", s.handleEndCoverage)
	mux.HandleFunc("POST /api/requests/{id}/resolve", s.handleResolveRequest)
	mux.HandleFunc("POST /api/requests/{id}/withdraw", s.handleWithdrawRequest)
	mux.HandleFunc("GET /api/inbox", s.handleInbox)
	mux.HandleFunc("GET /api/inbox/count", s.handleInboxCount)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/members/{id}/pin", s.handleSetPin)
	mux.HandleFunc("POST /api/cases/{no}/checklist", s.handleAddChecklistItem)
	mux.HandleFunc("PATCH /api/checklist/{id}", s.handlePatchChecklistItem)
	mux.HandleFunc("DELETE /api/checklist/{id}", s.handleDeleteChecklistItem)
	mux.HandleFunc("GET /api/obligations", s.handleListObligations)
	mux.HandleFunc("POST /api/obligations", s.handleCreateObligation)
	mux.HandleFunc("PATCH /api/obligations/{id}", s.handlePatchObligation)
	mux.HandleFunc("POST /api/obligations/{id}/spawn", s.handleSpawnObligation)
	mux.HandleFunc("POST /api/cases/{no}/make-recurring", s.handleMakeRecurring)
	mux.HandleFunc("GET /api/reports", s.handleReports)
	mux.HandleFunc("GET /api/calendar.ics", s.handleCalendarICS)
	mux.HandleFunc("POST /api/sample", s.handleSample)
	mux.HandleFunc("GET /api/export/examiner", s.handleExaminerExport)
	mux.HandleFunc("POST /api/records/refresh", s.handleRecordsRefresh)
	mux.HandleFunc("POST /api/backup", s.handleBackupNow)
	mux.HandleFunc("GET /api/import/template", s.handleImportTemplate)
	mux.HandleFunc("POST /api/import", s.handleImport)
}
