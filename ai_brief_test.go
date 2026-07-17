package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func createOwnedCase(t *testing.T, e *env) string {
	t.Helper()
	code, out := e.call("1", "POST", "/api/cases", map[string]any{
		"title": "Review reported record changes", "owner_id": 1,
	})
	e.must(200, code, out, "create AI test case")
	return caseOf(out)["case_no"].(string)
}

func TestAIBriefDemoReviewAndAudit(t *testing.T) {
	e := newEnv(t)
	e.setup()
	no := createOwnedCase(t, e)

	code, out := e.call("1", "POST", "/api/cases/"+no+"/ai-briefs", map[string]any{"demo": true})
	e.must(200, code, out, "create demo AI brief")
	brief := out["brief"].(map[string]any)
	if brief["model"] != "demo-fixture" {
		t.Fatalf("unexpected demo model: %v", brief["model"])
	}
	if brief["citation_count"].(float64) == 0 || brief["citation_count"] != brief["grounded_citations"] {
		t.Fatalf("demo citations should all be grounded: %v/%v", brief["grounded_citations"], brief["citation_count"])
	}
	briefID := int64(brief["id"].(float64))

	decisions := []map[string]any{
		{"item_id": "action-preserve", "decision": "ACCEPTED"},
		{"item_id": "gap-audit-log", "decision": "ACCEPTED"},
		{"item_id": "risk-record-integrity", "decision": "REJECTED"},
	}
	code, out = e.call("1", "POST", "/api/cases/"+no+"/ai-briefs/"+jsonNumber(briefID)+"/decisions", map[string]any{"decisions": decisions})
	e.must(200, code, out, "review AI brief")
	brief = out["brief"].(map[string]any)
	if brief["status"] != "REVIEWED" || len(brief["decisions"].([]any)) != 3 {
		t.Fatalf("unexpected review state: %v", brief)
	}

	code, detail := e.call("1", "GET", "/api/cases/"+no, nil)
	e.must(200, code, detail, "load reviewed case")
	checklist := detail["checklist"].([]any)
	labels := map[string]bool{}
	for _, raw := range checklist {
		item := raw.(map[string]any)
		labels[item["label"].(string)] = true
	}
	if !labels["Preserve the review-system audit logs and July 9 email"] || !labels["Obtain evidence: Obtain the review-system user audit log"] {
		t.Fatalf("accepted AI items were not applied to the checklist: %v", labels)
	}
	events := detail["events"].([]any)
	kinds := map[string]int{}
	for _, raw := range events {
		kinds[raw.(map[string]any)["kind"].(string)]++
	}
	if kinds["AI_PROPOSED"] != 1 || kinds["AI_ACCEPTED"] != 2 || kinds["AI_REJECTED"] != 1 {
		t.Fatalf("unexpected AI audit events: %v", kinds)
	}

	code, _ = e.call("1", "POST", "/api/cases/"+no+"/ai-briefs/"+jsonNumber(briefID)+"/decisions", map[string]any{"decisions": []map[string]any{{"item_id": "action-preserve", "decision": "ACCEPTED"}}})
	if code != 200 {
		t.Fatalf("same review decision should be idempotent, got %d", code)
	}
	code, _ = e.call("1", "POST", "/api/cases/"+no+"/ai-briefs/"+jsonNumber(briefID)+"/decisions", map[string]any{"decisions": []map[string]any{{"item_id": "action-preserve", "decision": "REJECTED"}}})
	if code != 409 {
		t.Fatalf("changing an immutable review decision should conflict, got %d", code)
	}
}

func TestAIBriefPermissionsAndMissingKey(t *testing.T) {
	e := newEnv(t)
	e.setup()
	no := createOwnedCase(t, e)
	code, _ := e.call("2", "POST", "/api/cases/"+no+"/ai-briefs", map[string]any{"demo": true})
	if code != http.StatusForbidden {
		t.Fatalf("non-owner should not run brief, got %d", code)
	}
	code, out := e.call("1", "POST", "/api/cases/"+no+"/ai-briefs", map[string]any{"report": "A reported issue."})
	if code != http.StatusServiceUnavailable || !strings.Contains(out["error"].(string), "OPENAI_API_KEY") {
		t.Fatalf("missing key should be explicit, got %d %v", code, out)
	}
}

func TestAIBriefUsesGPT56StructuredResponses(t *testing.T) {
	e := newEnv(t)
	e.setup()
	no := createOwnedCase(t, e)

	analysis := investigationBrief{
		ExecutiveSummary: "The report warrants a scoped review.", ScopeNote: "Human review required.",
		Allegations: []briefItem{{ID: "reported-issue", Title: "Reported issue", Detail: "A report was received.", Priority: "medium", Confidence: "high", Evidence: []briefCitation{{Source: "report", Quote: "A report was received on July 8."}}}},
		Timeline:    []briefItem{}, PolicyMatches: []briefItem{}, EvidenceGaps: []briefItem{}, Conflicts: []briefItem{},
		RiskFlags: []briefItem{}, RecommendedActions: []briefItem{}, ReviewQuestions: []briefItem{},
	}
	analysisJSON, _ := json.Marshal(analysis)
	var captured map[string]any
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing bearer token")
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &captured); err != nil {
			t.Errorf("bad request JSON: %v", err)
		}
		writeJSON(w, 200, map[string]any{
			"id": "resp_casebook_test", "model": "gpt-5.6-sol-2026-07-13", "status": "completed",
			"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": string(analysisJSON)}}}},
		})
	}))
	defer mock.Close()
	e.s.aiAPIKey = "test-key"
	e.s.aiBaseURL = mock.URL
	e.s.aiClient = mock.Client()

	code, out := e.call("1", "POST", "/api/cases/"+no+"/ai-briefs", map[string]any{
		"report": "A report was received on July 8.", "policy": "Policy review pending.", "evidence": "No additional evidence.",
	})
	e.must(200, code, out, "create live AI brief")
	brief := out["brief"].(map[string]any)
	if brief["model"] != "gpt-5.6-sol-2026-07-13" || brief["response_id"] != "resp_casebook_test" {
		t.Fatalf("response provenance was not preserved: %v", brief)
	}
	if captured["model"] != defaultAIModel || captured["store"] != false {
		t.Fatalf("wrong model or storage contract: %v", captured)
	}
	reasoning := captured["reasoning"].(map[string]any)
	if reasoning["effort"] != "medium" {
		t.Fatalf("reasoning effort not explicit: %v", reasoning)
	}
	textConfig := captured["text"].(map[string]any)
	format := textConfig["format"].(map[string]any)
	if format["type"] != "json_schema" || format["strict"] != true {
		t.Fatalf("structured output contract missing: %v", format)
	}
}

func jsonNumber(v int64) string { return strconv.FormatInt(v, 10) }
