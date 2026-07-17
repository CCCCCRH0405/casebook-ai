package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type workloadRow struct {
	Member     string `json:"member"`
	Open       int    `json:"open"`
	InProgress int    `json:"in_progress"`
	Waiting    int    `json:"waiting"`
	Overdue    int    `json:"overdue"`
}

func (s *server) handleReports(w http.ResponseWriter, r *http.Request) {
	if _, err := s.actor(r); err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	rows, err := s.db.Query(caseSelect + ` WHERE c.status NOT IN ('Completed','Archived','Cancelled')
ORDER BY CASE WHEN c.due_date='' THEN 1 ELSE 0 END, c.due_date`)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer rows.Close()
	td := today()
	waiting := []caseRow{}
	overdue := []caseRow{}
	loads := map[string]*workloadRow{}
	for rows.Next() {
		c, err := scanCase(rows)
		if err != nil {
			continue
		}
		name := c.OwnerName
		if name == "" {
			name = "(unassigned)"
		}
		l, ok := loads[name]
		if !ok {
			l = &workloadRow{Member: name}
			loads[name] = l
		}
		switch c.Status {
		case "Waiting":
			l.Waiting++
			waiting = append(waiting, c)
		case "In Progress":
			l.InProgress++
		default:
			l.Open++
		}
		if c.DueDate != "" && c.DueDate < td && c.Status != "Waiting" {
			l.Overdue++
			overdue = append(overdue, c)
		}
	}
	workload := []workloadRow{}
	for _, m := range s.listMembers() {
		if l, ok := loads[m.Name]; ok {
			workload = append(workload, *l)
		}
	}
	if l, ok := loads["(unassigned)"]; ok {
		workload = append(workload, *l)
	}
	writeJSON(w, 200, map[string]any{
		"waiting": nz(waiting), "overdue": nz(overdue), "workload": workload, "as_of": td,
	})
}

func icsEscape(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, ";", `\;`)
	v = strings.ReplaceAll(v, ",", `\,`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

func icsDate(d string) string { return strings.ReplaceAll(d, "-", "") }

func (s *server) handleCalendarICS(w http.ResponseWriter, r *http.Request) {
	memberID, _ := strconv.ParseInt(r.URL.Query().Get("member"), 10, 64)
	var b strings.Builder
	stamp := time.Now().UTC().Format("20060102T150405Z")
	team := getMeta(s.db, "team_name")
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Casebook//EN\r\nCALSCALE:GREGORIAN\r\n")
	b.WriteString("X-WR-CALNAME:Casebook — " + icsEscape(team) + "\r\n")
	event := func(uid, date, summary string) {
		if date == "" {
			return
		}
		b.WriteString("BEGIN:VEVENT\r\nUID:" + uid + "@casebook\r\nDTSTAMP:" + stamp + "\r\n")
		b.WriteString("DTSTART;VALUE=DATE:" + icsDate(date) + "\r\n")
		b.WriteString("SUMMARY:" + icsEscape(summary) + "\r\nEND:VEVENT\r\n")
	}
	q := caseSelect + ` WHERE c.status NOT IN ('Completed','Archived','Cancelled')`
	args := []any{}
	if memberID > 0 {
		q += ` AND (c.owner_id=? OR c.id IN (SELECT case_id FROM coverage WHERE coverer_id=? AND status='ACTIVE'))`
		args = append(args, memberID, memberID)
	}
	rows, err := s.db.Query(q, args...)
	if err == nil {
		for rows.Next() {
			c, err := scanCase(rows)
			if err != nil {
				continue
			}
			event("cb-"+c.CaseNo+"-due", c.DueDate, "["+c.CaseNo+"] "+c.Title)
			if c.Deadline != "" && c.Deadline != c.DueDate {
				event("cb-"+c.CaseNo+"-deadline", c.Deadline, "["+c.CaseNo+"] "+c.Title+" — HARD DEADLINE")
			}
		}
		rows.Close()
	}
	for _, o := range s.listObligations() {
		if o.Active && (memberID == 0 || o.OwnerID == memberID || o.OwnerID == 0) {
			event(fmt.Sprintf("cb-ob-%d-%s", o.ID, icsDate(o.NextDue)), o.NextDue, "[Recurring] "+o.Name)
		}
	}
	b.WriteString("END:VCALENDAR\r\n")
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="casebook.ics"`)
	w.Write([]byte(b.String()))
}

func (s *server) handleSample(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	var existing int
	s.db.QueryRow(`SELECT COUNT(*) FROM cases`).Scan(&existing)
	if existing > 3 {
		writeErr(w, 409, "sample data is only available in a near-empty workspace (this one already has cases)")
		return
	}
	td := time.Now()
	d := func(days int) string { return td.AddDate(0, 0, days).Format("2006-01-02") }
	// resolve ids BEFORE opening the tx: with a single SQLite connection,
	// any s.db query made while a tx is open deadlocks the process
	typesByName := map[string]int64{}
	for _, t := range s.listTypes() {
		typesByName[t.Name] = t.ID
	}
	typeID := func(name string) any {
		if id, ok := typesByName[name]; ok {
			return id
		}
		return nil
	}
	members := []member{}
	for _, m := range s.listMembers() {
		if m.Active {
			members = append(members, m)
		}
	}
	// member at a given offset from the actor; wraps for solo workspaces
	other := func(n int) int64 {
		if len(members) == 0 {
			return actor.ID
		}
		idx := 0
		for i, m := range members {
			if m.ID == actor.ID {
				idx = i
			}
		}
		return members[(idx+n)%len(members)].ID
	}
	mate := other(1) // a teammate (== actor only if solo)
	nameByID := map[int64]string{}
	for _, m := range members {
		nameByID[m.ID] = m.Name
	}

	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	type sample struct {
		title, typ, status, due, deadline, waitingOn, waitingDetail, source, tags string
		owner                                                                     any
		waitingSince                                                              string
		checklist                                                                 []string
		checkStates                                                               []string
		note                                                                      string
	}
	samples := []sample{
		{title: "Respond to regulator exam letter — trade records", typ: "Inquiry", status: "In Progress",
			due: d(2), deadline: d(9), owner: actor.ID, source: "Exam letter received by mail", tags: "exam,priority",
			checklist:   []string{"Trade blotter Q1", "Approval emails", "Account opening docs", "Supervisory review notes"},
			checkStates: []string{"Received", "Requested", "Needed", "Needed"},
			note:        "Sample selection agreed with the examiner; pulling Q1 first."},
		{title: "Quarterly account review sampling", typ: "Testing", status: "In Progress",
			due: d(0), owner: actor.ID, tags: "quarterly",
			checklist: []string{"Sampling memo", "Population extract", "Exceptions log"}, checkStates: []string{"Received", "Received", "Requested"}},
		{title: "Marketing material review — June batch", typ: "Review", status: "Waiting",
			due: d(-3), owner: actor.ID, waitingOn: "Business unit", waitingDetail: "final artwork from marketing", waitingSince: d(-12)},
		{title: "Update AML procedures — section 4 monitoring", typ: "Remediation", status: "In Progress",
			due: d(4), owner: actor.ID, note: "Drafting redline; will route to Legal for sign-off."},
		{title: "Best execution quarterly review", typ: "Testing", status: "Open", due: d(14), owner: actor.ID, tags: "quarterly"},
		{title: "OFAC screening exception — name match", typ: "Inquiry", status: "Waiting",
			due: d(1), owner: actor.ID, waitingOn: "Vendor", waitingDetail: "screening tool log export", waitingSince: d(-4)},

		{title: "Branch audit follow-up items", typ: "Remediation", status: "In Progress", due: d(6), owner: mate},
		{title: "Form U4 amendment review", typ: "Filing", status: "Open", due: d(3), owner: mate, tags: "registration"},
		{title: "Outside business activity disclosure review", typ: "Review", status: "Waiting",
			due: d(8), owner: mate, waitingOn: "Legal", waitingDetail: "opinion on dual registration", waitingSince: d(-7)},
		{title: "Annual 3120 supervisory controls testing", typ: "Testing", status: "Open", due: d(21), owner: mate, tags: "annual"},

		{title: "New vendor due diligence questionnaire", typ: "Request", status: "Open", due: d(11)},
		{title: "Gift & entertainment log reconciliation", typ: "Review", status: "Open", due: d(18), owner: other(2)},
		{title: "Email surveillance lexicon update", typ: "Task", status: "Open", due: d(25), owner: mate},

		{title: "Quarterly attestation collection — Q1", typ: "Filing", status: "Completed", due: d(-6), owner: actor.ID, tags: "quarterly"},
		{title: "Trade error escalation — wrong account", typ: "Inquiry", status: "Completed", due: d(-15), owner: mate},
	}

	caseIDByTitle := map[string]int64{}
	for _, sm := range samples {
		no, err := nextCaseNo(tx)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		ownerArg := sm.owner
		completedAt := ""
		if sm.status == "Completed" {
			completedAt = nowStamp()
		}
		res, err := tx.Exec(`INSERT INTO cases(case_no,title,case_type_id,status,owner_id,due_date,deadline,waiting_on,waiting_detail,waiting_since,source,tags,created_at,created_by,updated_at,completed_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			no, sm.title, typeID(sm.typ), sm.status, ownerArg, sm.due, sm.deadline, sm.waitingOn, sm.waitingDetail, sm.waitingSince,
			sm.source, sm.tags, nowStamp(), actor.ID, nowStamp(), completedAt)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		caseID, _ := res.LastInsertId()
		caseIDByTitle[sm.title] = caseID
		creator := actor.ID
		if id, ok := sm.owner.(int64); ok && id != 0 {
			creator = id
		}
		if err := addEvent(tx, caseID, creator, "CREATED", map[string]any{"title": sm.title, "sample": true}); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		for i, item := range sm.checklist {
			state := "Needed"
			if i < len(sm.checkStates) {
				state = sm.checkStates[i]
			}
			if _, err := tx.Exec(`INSERT INTO checklist_items(case_id,label,state,sort,created_at) VALUES(?,?,?,?,?)`,
				caseID, item, state, i+1, nowStamp()); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
		}
		if sm.note != "" {
			if err := addEvent(tx, caseID, creator, "NOTE", map[string]any{"text": sm.note}); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
		}
	}

	obligations := []struct {
		name, typ, freq, due string
		lead                 int
		checklist            string
	}{
		{"Annual compliance meeting", "Task", "Yearly", d(45), 60, `["Agenda","Board pre-read","Sign-in sheet","Minutes"]`},
		{"Quarterly 3120 report to CEO", "Filing", "Quarterly", d(20), 30, `["Testing summary","Exceptions appendix","CEO sign-off"]`},
		{"Monthly AML alert clearing", "Review", "Monthly", d(7), 7, `["Alert export","Disposition notes"]`},
		{"Annual CEO certification (3130)", "Filing", "Yearly", d(120), 45, `["Draft certification","Supporting WSP cites","Signature"]`},
	}
	for _, o := range obligations {
		if _, err := tx.Exec(`INSERT INTO obligations(name,case_type_id,default_owner_id,frequency,next_due,lead_days,checklist,active,created_at,created_by)
VALUES(?,?,?,?,?,?,?,1,?,?)`,
			o.name, typeID(o.typ), actor.ID, o.freq, o.due, o.lead, o.checklist, nowStamp(), actor.ID); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}

	// collaboration: only meaningful when there is a real teammate
	if mate != actor.ID {
		// an active coverage: the teammate is covering one of the actor's cases
		if cid, ok := caseIDByTitle["Best execution quarterly review"]; ok {
			if _, err := tx.Exec(`INSERT INTO coverage(case_id,owner_id,coverer_id,start_date,status,created_at) VALUES(?,?,?,?,'ACTIVE',?)`,
				cid, actor.ID, mate, d(-1), nowStamp()); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			addEvent(tx, cid, mate, "COVERAGE", map[string]any{"action": "started", "coverer": nameByID[mate], "owner": actor.Name})
		}
		// two open requests addressed TO the actor, so the Inbox has something
		if cid, ok := caseIDByTitle["Branch audit follow-up items"]; ok {
			if _, err := tx.Exec(`INSERT INTO requests(case_id,kind,from_id,to_id,message,status,created_at) VALUES(?,?,?,?,?,'OPEN',?)`,
				cid, "HELP", mate, actor.ID, "Can you sanity-check my exception write-up before I close this?", nowStamp()); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			addEvent(tx, cid, mate, "REQUEST", map[string]any{"kind": "HELP", "to": actor.Name, "message": "Can you sanity-check my exception write-up before I close this?"})
		}
		if cid, ok := caseIDByTitle["Outside business activity disclosure review"]; ok {
			if _, err := tx.Exec(`INSERT INTO requests(case_id,kind,from_id,to_id,message,status,created_at) VALUES(?,?,?,?,?,'OPEN',?)`,
				cid, "COVERAGE", mate, actor.ID, "Out Thu–Fri — can you cover this if Legal replies?", nowStamp()); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			addEvent(tx, cid, mate, "REQUEST", map[string]any{"kind": "COVERAGE", "to": actor.Name, "message": "Out Thu–Fri — can you cover this if Legal replies?"})
		}
	}

	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "created": len(samples)})
}
