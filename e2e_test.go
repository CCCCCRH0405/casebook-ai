package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

type env struct {
	t   *testing.T
	srv *httptest.Server
	s   *server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ws := t.TempDir()
	for _, d := range []string{"Records", "Backups", "Imports"} {
		if err := os.MkdirAll(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := openStore(filepath.Join(ws, "workspace.cbk"))
	if err != nil {
		t.Fatal(err)
	}
	s := &server{db: db, ws: ws}
	mux := http.NewServeMux()
	s.routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(func() { db.Close() })
	return &env{t: t, srv: srv, s: s}
}

func (e *env) call(member, method, path string, body any) (int, map[string]any) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if member != "" {
		req.Header.Set("X-Member", member)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := map[string]any{}
	json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

func (e *env) must(code int, gotCode int, got map[string]any, ctx string) {
	e.t.Helper()
	if gotCode != code {
		e.t.Fatalf("%s: want %d got %d (%v)", ctx, code, gotCode, got)
	}
}

func (e *env) setup() {
	code, out := e.call("", "POST", "/api/setup", map[string]any{
		"team": "Compliance", "you": "Dana Mori", "members": []string{"Alex Kim", "Priya Shah"},
	})
	e.must(200, code, out, "setup")
}

func caseOf(out map[string]any) map[string]any {
	c, _ := out["case"].(map[string]any)
	return c
}

func TestCaseLifecycleAndPermissions(t *testing.T) {
	e := newEnv(t)
	e.setup()

	code, out := e.call("", "POST", "/api/cases", map[string]any{"title": "x"})
	e.must(401, code, out, "create without identity")

	code, out = e.call("1", "POST", "/api/cases", map[string]any{"title": "Exam response", "owner_id": 1, "due_date": today()})
	e.must(200, code, out, "create")
	no := caseOf(out)["case_no"].(string)
	if no != "CB-"+fmt.Sprint(time.Now().Year())+"-0001" {
		t.Fatalf("unexpected case no %s", no)
	}

	code, out = e.call("1", "POST", "/api/cases/"+no+"/status", map[string]any{"to": "Waiting"})
	e.must(400, code, out, "waiting without waiting_on")

	code, out = e.call("1", "POST", "/api/cases/"+no+"/status", map[string]any{"to": "Waiting", "waiting_on": "Legal"})
	e.must(200, code, out, "waiting ok")

	code, out = e.call("1", "POST", "/api/cases/"+no+"/status", map[string]any{"to": "Archived"})
	e.must(409, code, out, "illegal transition")

	code, out = e.call("2", "PATCH", "/api/cases/"+no, map[string]any{"title": "hijack"})
	e.must(403, code, out, "non-owner edit blocked")

	code, out = e.call("2", "POST", "/api/cases/"+no+"/notes", map[string]any{"text": "fyi"})
	e.must(200, code, out, "non-owner note allowed")

	code, out = e.call("1", "POST", "/api/cases/"+no+"/status", map[string]any{"to": "Completed"})
	e.must(200, code, out, "complete")
	if caseOf(out)["waiting_on"].(string) != "" {
		t.Fatal("waiting_on not cleared on completion")
	}

	code, out = e.call("1", "POST", "/api/cases/"+no+"/status", map[string]any{"to": "In Progress"})
	e.must(200, code, out, "reopen")
	if caseOf(out)["completed_at"].(string) != "" {
		t.Fatal("completed_at not cleared on reopen")
	}

	code, out = e.call("1", "POST", "/api/cases/"+no+"/status", map[string]any{"to": "Cancelled"})
	e.must(400, code, out, "cancel without reason")
}

func TestClaimAndRequests(t *testing.T) {
	e := newEnv(t)
	e.setup()
	_, out := e.call("1", "POST", "/api/cases", map[string]any{"title": "Pool item"})
	no := caseOf(out)["case_no"].(string)

	code, out := e.call("3", "POST", "/api/cases/"+no+"/claim", nil)
	e.must(200, code, out, "claim")
	code, out = e.call("2", "POST", "/api/cases/"+no+"/claim", nil)
	e.must(409, code, out, "double claim")

	// coverage: owner(3) asks 2
	code, out = e.call("3", "POST", "/api/cases/"+no+"/requests", map[string]any{"kind": "COVERAGE", "to_id": 2, "message": "out next week"})
	e.must(200, code, out, "coverage request")
	code, out = e.call("3", "POST", "/api/cases/"+no+"/requests", map[string]any{"kind": "COVERAGE", "to_id": 2})
	e.must(409, code, out, "duplicate open request")
	code, out = e.call("1", "POST", "/api/requests/1/resolve", map[string]any{"decision": "ACCEPTED"})
	e.must(403, code, out, "resolve by wrong member")
	code, out = e.call("2", "POST", "/api/requests/1/resolve", map[string]any{"decision": "ACCEPTED"})
	e.must(200, code, out, "accept coverage")

	code, out = e.call("2", "PATCH", "/api/cases/"+no, map[string]any{"due_date": today()})
	e.must(200, code, out, "coverer can edit")
	code, out = e.call("1", "PATCH", "/api/cases/"+no, map[string]any{"due_date": today()})
	e.must(403, code, out, "third party still blocked")

	// transfer: 1 requests, owner 3 approves
	code, out = e.call("1", "POST", "/api/cases/"+no+"/requests", map[string]any{"kind": "TRANSFER", "message": "mine"})
	e.must(200, code, out, "transfer request")
	code, out = e.call("3", "POST", "/api/requests/2/resolve", map[string]any{"decision": "ACCEPTED"})
	e.must(200, code, out, "accept transfer")
	_, out = e.call("1", "GET", "/api/cases/"+no, nil)
	c := caseOf(out)
	if c["owner_name"].(string) != "Dana Mori" {
		t.Fatalf("transfer did not move ownership: %v", c["owner_name"])
	}
	if c["coverer_name"].(string) != "" {
		t.Fatal("coverage not ended on transfer")
	}
	code, out = e.call("3", "POST", "/api/requests/2/resolve", map[string]any{"decision": "ACCEPTED"})
	e.must(409, code, out, "stale resolve")
}

func TestPinChecklistRecurring(t *testing.T) {
	e := newEnv(t)
	e.setup()
	code, out := e.call("1", "POST", "/api/members/1/pin", map[string]any{"pin": "2468", "old_pin": ""})
	e.must(200, code, out, "set pin")
	code, out = e.call("1", "POST", "/api/cases", map[string]any{"title": "x"})
	e.must(401, code, out, "no pin blocked")

	req, _ := http.NewRequest("POST", e.srv.URL+"/api/cases", strings.NewReader(`{"title":"Annual cert","owner_id":1,"due_date":"`+today()+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Member", "1")
	req.Header.Set("X-Pin", "2468")
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != 200 {
		t.Fatalf("pin create failed: %d", res.StatusCode)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var created map[string]any
	json.Unmarshal(raw, &created)
	no := caseOf(created)["case_no"].(string)

	// clear pin so plain calls work for the rest
	code, out = e.call("", "POST", "/api/login", map[string]any{"member_id": 1, "pin": "0000"})
	e.must(401, code, out, "wrong pin login")
	pinReq, _ := http.NewRequest("POST", e.srv.URL+"/api/members/1/pin", strings.NewReader(`{"pin":"","old_pin":"2468"}`))
	pinReq.Header.Set("Content-Type", "application/json")
	pinReq.Header.Set("X-Member", "1")
	pinReq.Header.Set("X-Pin", "2468")
	pres, _ := http.DefaultClient.Do(pinReq)
	if pres.StatusCode != 200 {
		t.Fatalf("remove pin failed: %d", pres.StatusCode)
	}
	pres.Body.Close()

	for _, l := range []string{"Signed cert", "Minutes"} {
		code, out = e.call("1", "POST", "/api/cases/"+no+"/checklist", map[string]any{"label": l})
		e.must(200, code, out, "checklist add")
	}
	code, out = e.call("1", "PATCH", "/api/checklist/1", map[string]any{"state": "Done"})
	e.must(400, code, out, "invalid checklist state")
	code, out = e.call("2", "PATCH", "/api/checklist/1", map[string]any{"state": "Received"})
	e.must(403, code, out, "checklist non-owner blocked")
	code, out = e.call("1", "PATCH", "/api/checklist/1", map[string]any{"state": "Received"})
	e.must(200, code, out, "checklist advance")

	nextYear := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	code, out = e.call("1", "POST", "/api/cases/"+no+"/make-recurring", map[string]any{
		"frequency": "Yearly", "next_due": nextYear, "lead_days": 30,
	})
	e.must(200, code, out, "make recurring")
	code, out = e.call("1", "POST", "/api/obligations/1/spawn", nil)
	e.must(200, code, out, "manual spawn")
	spawnedNo := out["case_no"].(string)
	_, got := e.call("1", "GET", "/api/cases/"+spawnedNo, nil)
	items, _ := got["checklist"].([]any)
	if len(items) != 2 {
		t.Fatalf("spawned case checklist template missing: %d items", len(items))
	}
	if caseOf(got)["due_date"].(string) != nextYear {
		t.Fatal("spawned case due date wrong")
	}
}

func TestSpawnOccurrenceDedup(t *testing.T) {
	e := newEnv(t)
	e.setup()
	due := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	_, out := e.call("1", "POST", "/api/cases", map[string]any{"title": "Monthly review", "owner_id": 1, "due_date": due})
	no := caseOf(out)["case_no"].(string)
	// next_due deliberately equals the linked case's own due date → occurrence already covered
	code, out := e.call("1", "POST", "/api/cases/"+no+"/make-recurring", map[string]any{
		"frequency": "Monthly", "next_due": due, "lead_days": 10,
	})
	e.must(200, code, out, "make recurring")

	code, out = e.call("1", "POST", "/api/obligations/1/spawn", nil)
	e.must(409, code, out, "duplicate occurrence must not spawn a second case")

	// the schedule must have advanced, so the following spawn creates the NEXT month
	code, out = e.call("1", "POST", "/api/obligations/1/spawn", nil)
	e.must(200, code, out, "next occurrence spawns")
	base, _ := time.Parse("2006-01-02", due)
	nextDue := base.AddDate(0, 1, 0).Format("2006-01-02")
	_, got := e.call("1", "GET", "/api/cases/"+out["case_no"].(string), nil)
	if caseOf(got)["due_date"].(string) != nextDue {
		t.Fatalf("expected next occurrence due %s, got %s", nextDue, caseOf(got)["due_date"])
	}
	// engine pass must not create duplicates either
	runSpawner(e.s.db)
	_, list := e.call("1", "GET", "/api/cases?view=all", nil)
	cases, _ := list["cases"].([]any)
	seen := map[string]int{}
	for _, raw := range cases {
		c := raw.(map[string]any)
		if c["obligation_id"].(float64) > 0 {
			seen[c["due_date"].(string)]++
		}
	}
	for d, n := range seen {
		if n > 1 {
			t.Fatalf("duplicate cases for occurrence %s: %d", d, n)
		}
	}
}

func TestImportReportsExportSample(t *testing.T) {
	e := newEnv(t)
	e.setup()

	f := excelize.NewFile()
	f.SetSheetName("Sheet1", "Cases")
	rows := [][]any{
		{"Title", "Type", "Owner", "Status", "Due", "Deadline", "Waiting on", "Source", "Location", "Tags", "Description"},
		{"Good row", "Inquiry", "Alex Kim", "Open", "2026-07-01", "", "", "", "", "", ""},
		{"", "Inquiry", "", "", "", "", "", "", "", "", "broken row, no title"},
		{"Bad type row", "Nope", "", "Waiting", "", "", "", "", "", "", ""},
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		f.SetSheetRow("Cases", cell, &r)
	}
	upload := func(commit bool) (int, map[string]any) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", "import.xlsx")
		f.Write(fw)
		mw.Close()
		url := e.srv.URL + "/api/import"
		if commit {
			url += "?commit=1"
		}
		req, _ := http.NewRequest("POST", url, &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Member", "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		out := map[string]any{}
		json.Unmarshal(raw, &out)
		return res.StatusCode, out
	}
	code, out := upload(true)
	e.must(200, code, out, "import dry result")
	if out["committed"].(bool) {
		t.Fatal("import committed despite errors")
	}
	if int(out["error_count"].(float64)) < 3 {
		t.Fatalf("expected >=3 errors, got %v", out["error_count"])
	}

	// fix the file and commit
	f.RemoveRow("Cases", 4)
	f.RemoveRow("Cases", 3)
	code, out = upload(true)
	e.must(200, code, out, "import commit")
	if !out["committed"].(bool) {
		t.Fatalf("clean import did not commit: %v", out)
	}

	code, out = e.call("1", "GET", "/api/reports", nil)
	e.must(200, code, out, "reports")

	res, err := http.Get(e.srv.URL + "/api/calendar.ics?member=1")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("ics failed: %v %v", err, res.StatusCode)
	}
	ics, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(ics), "BEGIN:VCALENDAR") {
		t.Fatal("ics missing envelope")
	}

	req, _ := http.NewRequest("GET", e.srv.URL+"/api/export/examiner?member=1", nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("examiner export failed: %v %v", err, res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if _, err := excelize.OpenReader(bytes.NewReader(body)); err != nil {
		t.Fatalf("examiner export not a valid workbook: %v", err)
	}

	for i := 0; i < 4; i++ {
		e.call("1", "POST", "/api/cases", map[string]any{"title": fmt.Sprintf("filler %d", i)})
	}
	code, out = e.call("1", "POST", "/api/sample", nil)
	e.must(409, code, out, "sample gate on used workspace")
}

func TestSampleOnFreshWorkspace(t *testing.T) {
	e := newEnv(t)
	e.setup()
	code, out := e.call("1", "POST", "/api/sample", nil)
	e.must(200, code, out, "sample load")
	_, list := e.call("1", "GET", "/api/cases?view=all", nil)
	cases, _ := list["cases"].([]any)
	if len(cases) < 5 {
		t.Fatalf("sample created too few cases: %d", len(cases))
	}
	code, out = e.call("1", "GET", "/api/today", nil)
	e.must(200, code, out, "today after sample")
}
