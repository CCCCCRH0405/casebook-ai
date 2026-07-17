package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

var importHeaders = []string{"Title", "Type", "Owner", "Status", "Next action due (YYYY-MM-DD)",
	"Hard deadline (YYYY-MM-DD)", "Waiting on", "Source", "Location", "Tags", "Description"}

func (s *server) handleImportTemplate(w http.ResponseWriter, r *http.Request) {
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", "Cases")
	sheetWithHeaders(f, "Cases", importHeaders)
	appendRow(f, "Cases", 2, []any{"Respond to exam request — trade records Q1", "Inquiry", "", "Open",
		"2026-07-15", "2026-07-31", "", "Exam letter dated …", `S:\Compliance\Exam2026`, "exam, trading", "Example row — delete me"})
	sheetWithHeaders(f, "Instructions", []string{"How to use this template"})
	lines := []string{
		"Fill the Cases sheet, one case per row. Only Title is required.",
		"Leave any other cell blank to use the default.",
		"Then upload the file on the Import page. Nothing is created until every row passes validation.",
		"",
		"Valid values:",
		"Type: " + strings.Join(s.typeNames(), ", "),
		"Owner: " + strings.Join(s.memberNames(), ", ") + " (blank = unassigned)",
		"Status: Open, In Progress, Waiting, Completed (blank = Open)",
		"Waiting on: " + strings.Join(waitingOptions, ", ") + " (required when Status is Waiting)",
		"Dates: YYYY-MM-DD",
	}
	for i, l := range lines {
		appendRow(f, "Instructions", i+2, []any{l})
	}
	f.SetColWidth("Cases", "A", "A", 42)
	f.SetColWidth("Cases", "B", "K", 18)
	f.SetColWidth("Instructions", "A", "A", 90)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="casebook-import-template.xlsx"`)
	f.Write(w)
}

func (s *server) typeNames() []string {
	out := []string{}
	for _, t := range s.listTypes() {
		if t.Active {
			out = append(out, t.Name)
		}
	}
	return out
}

func (s *server) memberNames() []string {
	out := []string{}
	for _, m := range s.listMembers() {
		if m.Active {
			out = append(out, m.Name)
		}
	}
	return out
}

type importRow struct {
	RowNo  int               `json:"row_no"`
	Data   map[string]string `json:"data"`
	Errors []string          `json:"errors"`
	typeID int64
	owner  int64
}

var importStatuses = map[string]bool{"Open": true, "In Progress": true, "Waiting": true, "Completed": true}

func normDate(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", true
	}
	for _, layout := range []string{"2006-01-02", "01/02/2006", "1/2/2006", "2006/01/02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	// excelize may surface date cells as serial numbers already formatted; reject anything else
	return v, false
}

func (s *server) parseImport(r *http.Request) ([]importRow, error) {
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		return nil, fmt.Errorf("could not read the upload: %v", err)
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("no file was attached — pick the filled-in template")
	}
	defer file.Close()
	f, err := excelize.OpenReader(file)
	if err != nil {
		return nil, fmt.Errorf("that file is not a readable .xlsx workbook")
	}
	defer f.Close()
	sheet := "Cases"
	if idx, _ := f.GetSheetIndex(sheet); idx == -1 {
		sheet = f.GetSheetName(0)
	}
	raw, err := f.GetRows(sheet)
	if err != nil || len(raw) < 1 {
		return nil, fmt.Errorf("the workbook has no rows to import")
	}
	types := map[string]int64{}
	for _, t := range s.listTypes() {
		types[strings.ToLower(t.Name)] = t.ID
	}
	members := map[string]int64{}
	for _, m := range s.listMembers() {
		if m.Active {
			members[strings.ToLower(m.Name)] = m.ID
		}
	}
	cell := func(row []string, i int) string {
		if i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	out := []importRow{}
	for i, row := range raw[1:] {
		ir := importRow{RowNo: i + 2, Data: map[string]string{}, Errors: []string{}}
		title := cell(row, 0)
		typeName := cell(row, 1)
		ownerName := cell(row, 2)
		status := cell(row, 3)
		due := cell(row, 4)
		deadline := cell(row, 5)
		waitingOn := cell(row, 6)
		empty := title == "" && typeName == "" && ownerName == "" && status == "" && due == "" &&
			deadline == "" && waitingOn == "" && cell(row, 7) == "" && cell(row, 8) == "" &&
			cell(row, 9) == "" && cell(row, 10) == ""
		if empty {
			continue
		}
		ir.Data["title"] = title
		ir.Data["type"] = typeName
		ir.Data["owner"] = ownerName
		ir.Data["status"] = status
		ir.Data["due_date"] = due
		ir.Data["deadline"] = deadline
		ir.Data["waiting_on"] = waitingOn
		ir.Data["source"] = cell(row, 7)
		ir.Data["location"] = cell(row, 8)
		ir.Data["tags"] = cell(row, 9)
		ir.Data["description"] = cell(row, 10)
		if title == "" {
			ir.Errors = append(ir.Errors, "Title is required")
		}
		if typeName != "" {
			if id, ok := types[strings.ToLower(typeName)]; ok {
				ir.typeID = id
			} else {
				ir.Errors = append(ir.Errors, "Unknown type: "+typeName)
			}
		}
		if ownerName != "" && !strings.EqualFold(ownerName, "Unassigned") {
			if id, ok := members[strings.ToLower(ownerName)]; ok {
				ir.owner = id
			} else {
				ir.Errors = append(ir.Errors, "Unknown owner: "+ownerName)
			}
		}
		if status == "" {
			ir.Data["status"] = "Open"
		} else if !importStatuses[status] {
			ir.Errors = append(ir.Errors, "Invalid status: "+status+" (use Open, In Progress, Waiting or Completed)")
		}
		if d, ok := normDate(due); ok {
			ir.Data["due_date"] = d
		} else {
			ir.Errors = append(ir.Errors, "Bad date in Next action due: "+due)
		}
		if d, ok := normDate(deadline); ok {
			ir.Data["deadline"] = d
		} else {
			ir.Errors = append(ir.Errors, "Bad date in Hard deadline: "+deadline)
		}
		if ir.Data["status"] == "Waiting" && waitingOn == "" {
			ir.Errors = append(ir.Errors, "Waiting on is required when Status is Waiting")
		}
		if waitingOn != "" && ir.Data["status"] != "Waiting" {
			ir.Errors = append(ir.Errors, "Waiting on is set but Status is not Waiting")
		}
		out = append(out, ir)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no data rows found below the header")
	}
	if len(out) > 1000 {
		return nil, fmt.Errorf("more than 1000 rows — split the file")
	}
	return out, nil
}

func (s *server) handleImport(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	rows, err := s.parseImport(r)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	errCount := 0
	for _, ir := range rows {
		errCount += len(ir.Errors)
	}
	commit := r.URL.Query().Get("commit") == "1"
	if !commit || errCount > 0 {
		writeJSON(w, 200, map[string]any{
			"rows": rows, "ok_count": len(rows), "error_count": errCount, "committed": false,
		})
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	created := []string{}
	for _, ir := range rows {
		no, err := nextCaseNo(tx)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		var typeArg, ownerArg any
		if ir.typeID > 0 {
			typeArg = ir.typeID
		}
		if ir.owner > 0 {
			ownerArg = ir.owner
		}
		status := ir.Data["status"]
		// imported Completed rows keep completed_at empty on purpose: they are usually
		// historical records, and stamping "now" would auto-archive them 7 days later
		completedAt := ""
		waitingOn, waitingSince := "", ""
		if status == "Waiting" {
			waitingOn = ir.Data["waiting_on"]
			waitingSince = today()
		}
		res, err := tx.Exec(`INSERT INTO cases(case_no,title,case_type_id,status,owner_id,due_date,deadline,
waiting_on,waiting_since,source,location,tags,description,created_at,created_by,updated_at,completed_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			no, ir.Data["title"], typeArg, status, ownerArg, ir.Data["due_date"], ir.Data["deadline"],
			waitingOn, waitingSince, ir.Data["source"], ir.Data["location"], ir.Data["tags"], ir.Data["description"],
			nowStamp(), actor.ID, nowStamp(), completedAt)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		caseID, _ := res.LastInsertId()
		if err := addEvent(tx, caseID, actor.ID, "IMPORTED", map[string]any{
			"row": ir.RowNo, "title": ir.Data["title"],
		}); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		created = append(created, no)
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"committed": true, "created": created, "ok_count": len(created), "error_count": 0})
}
