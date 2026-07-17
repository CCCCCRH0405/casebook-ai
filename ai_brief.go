package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAIModel     = "gpt-5.6-sol"
	openAIResponsesURL = "https://api.openai.com/v1/responses"
	maxSourceChars     = 50_000
	maxPacketChars     = 120_000
)

var briefIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type briefInputs struct {
	Report   string `json:"report"`
	Policy   string `json:"policy"`
	Evidence string `json:"evidence"`
}

type briefCitation struct {
	Source   string `json:"source"`
	Quote    string `json:"quote"`
	Grounded bool   `json:"grounded,omitempty"`
}

type briefItem struct {
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	Detail     string          `json:"detail"`
	Priority   string          `json:"priority"`
	Confidence string          `json:"confidence"`
	Evidence   []briefCitation `json:"evidence"`
}

type investigationBrief struct {
	ExecutiveSummary   string      `json:"executive_summary"`
	ScopeNote          string      `json:"scope_note"`
	Allegations        []briefItem `json:"allegations"`
	Timeline           []briefItem `json:"timeline"`
	PolicyMatches      []briefItem `json:"policy_matches"`
	EvidenceGaps       []briefItem `json:"evidence_gaps"`
	Conflicts          []briefItem `json:"conflicts"`
	RiskFlags          []briefItem `json:"risk_flags"`
	RecommendedActions []briefItem `json:"recommended_actions"`
	ReviewQuestions    []briefItem `json:"review_questions"`
}

type aiBriefDecision struct {
	ItemKind  string `json:"item_kind"`
	ItemID    string `json:"item_id"`
	ItemTitle string `json:"item_title"`
	Decision  string `json:"decision"`
	DecidedAt string `json:"decided_at"`
	DecidedBy string `json:"decided_by"`
	AppliedTo string `json:"applied_to"`
}

type aiBriefRecord struct {
	ID                int64              `json:"id"`
	CreatedAt         string             `json:"created_at"`
	CreatedBy         string             `json:"created_by"`
	Model             string             `json:"model"`
	ResponseID        string             `json:"response_id"`
	Status            string             `json:"status"`
	InputSHA256       string             `json:"input_sha256"`
	CitationCount     int                `json:"citation_count"`
	GroundedCitations int                `json:"grounded_citations"`
	Analysis          investigationBrief `json:"analysis"`
	Decisions         []aiBriefDecision  `json:"decisions"`
}

type openAIResponse struct {
	ID     string `json:"id"`
	Model  string `json:"model"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Refusal string `json:"refusal"`
		} `json:"content"`
	} `json:"output"`
}

type generatedBrief struct {
	Analysis   investigationBrief
	Model      string
	ResponseID string
}

func (s *server) aiModel() string {
	if v := strings.TrimSpace(os.Getenv("CASEBOOK_AI_MODEL")); v != "" {
		return v
	}
	return defaultAIModel
}

func (s *server) aiAPIKeyValue() string {
	if s.aiAPIKey != "" {
		return s.aiAPIKey
	}
	return strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
}

func (s *server) aiURL() string {
	if s.aiBaseURL != "" {
		return s.aiBaseURL
	}
	return openAIResponsesURL
}

func (s *server) aiHTTP() *http.Client {
	if s.aiClient != nil {
		return s.aiClient
	}
	return &http.Client{Timeout: 120 * time.Second}
}

func (s *server) aiStatus() map[string]any {
	return map[string]any{
		"configured":     s.aiAPIKeyValue() != "",
		"model":          s.aiModel(),
		"demo_available": true,
		"cloud_notice":   "Only text submitted from the AI Brief tab is sent to OpenAI. Casebook never analyzes a case automatically.",
	}
}

func (s *server) handleListAIBriefs(w http.ResponseWriter, r *http.Request) {
	if _, err := s.actor(r); err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	c, err := s.caseByNo(r.PathValue("no"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	briefs, err := s.aiBriefsForCase(c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load AI briefs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"briefs": briefs, "ai": s.aiStatus()})
}

func (s *server) handleCreateAIBrief(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	c, err := s.caseByNo(r.PathValue("no"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	if !s.canEdit(c, actor) {
		writeErr(w, http.StatusForbidden, "only the owner or active coverer can run an AI brief")
		return
	}
	var req struct {
		Report   string `json:"report"`
		Policy   string `json:"policy"`
		Evidence string `json:"evidence"`
		Demo     bool   `json:"demo"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	inputs := briefInputs{
		Report: strings.TrimSpace(req.Report), Policy: strings.TrimSpace(req.Policy), Evidence: strings.TrimSpace(req.Evidence),
	}
	if req.Demo && inputs.Report == "" {
		inputs = demoBriefInputs()
	}
	if err := validateBriefInputs(inputs); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	var generated generatedBrief
	if req.Demo {
		generated = generatedBrief{Analysis: demoInvestigationBrief(), Model: "demo-fixture", ResponseID: "local-demo"}
	} else {
		if s.aiAPIKeyValue() == "" {
			writeErr(w, http.StatusServiceUnavailable, "OPENAI_API_KEY is not configured; use the clearly labeled demo fixture or set the key before starting Casebook")
			return
		}
		generated, err = s.generateInvestigationBrief(r.Context(), actor, c, inputs)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
	}

	citations, grounded := normalizeAndGroundBrief(&generated.Analysis, inputs)
	inputJSON, _ := json.Marshal(inputs)
	analysisJSON, err := json.Marshal(generated.Analysis)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not store AI analysis")
		return
	}
	inputHash := sha256.Sum256(inputJSON)
	inputSHA := hex.EncodeToString(inputHash[:])

	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO ai_briefs(case_id,created_at,created_by,model,response_id,status,input_sha256,input_json,analysis_json,citation_count,grounded_citations)
VALUES(?,?,?,?,?,'READY',?,?,?,?,?)`, c.ID, nowStamp(), actor.ID, generated.Model, generated.ResponseID, inputSHA, string(inputJSON), string(analysisJSON), citations, grounded)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not store AI brief")
		return
	}
	briefID, _ := res.LastInsertId()
	if err := addEvent(tx, c.ID, actor.ID, "AI_PROPOSED", map[string]any{
		"brief_id": briefID, "model": generated.Model, "response_id": generated.ResponseID,
		"input_sha256": inputSHA, "summary": generated.Analysis.ExecutiveSummary,
		"citation_count": citations, "grounded_citations": grounded,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not append AI audit event")
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	record, err := s.aiBriefByID(c.ID, briefID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "AI brief created but could not be reloaded")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"brief": record})
}

func validateBriefInputs(in briefInputs) error {
	if in.Report == "" {
		return errors.New("report or intake narrative is required")
	}
	for label, value := range map[string]string{"report": in.Report, "policy": in.Policy, "evidence": in.Evidence} {
		if len(value) > maxSourceChars {
			return fmt.Errorf("%s text is too long (maximum %d characters)", label, maxSourceChars)
		}
	}
	if len(in.Report)+len(in.Policy)+len(in.Evidence) > maxPacketChars {
		return fmt.Errorf("source packet is too long (maximum %d characters)", maxPacketChars)
	}
	return nil
}

func (s *server) generateInvestigationBrief(ctx context.Context, actor member, c caseRow, inputs briefInputs) (generatedBrief, error) {
	packet := fmt.Sprintf("CASE\nNumber: %s\nTitle: %s\nCurrent status: %s\n\nREPORT OR INTAKE NARRATIVE\n%s\n\nPOLICY MATERIAL\n%s\n\nEVIDENCE NOTES\n%s",
		c.CaseNo, c.Title, c.Status, inputs.Report, valueOrMissing(inputs.Policy), valueOrMissing(inputs.Evidence))
	body := map[string]any{
		"model":             s.aiModel(),
		"store":             false,
		"reasoning":         map[string]any{"effort": "medium"},
		"instructions":      investigationInstructions,
		"input":             packet,
		"max_output_tokens": 10_000,
		"safety_identifier": s.safetyIdentifier(actor.ID),
		"text": map[string]any{
			"verbosity": "medium",
			"format": map[string]any{
				"type": "json_schema", "name": "casebook_investigation_brief", "strict": true,
				"schema": briefJSONSchema(),
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return generatedBrief{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.aiURL(), bytes.NewReader(raw))
	if err != nil {
		return generatedBrief{}, err
	}
	req.Header.Set("Authorization", "Bearer "+s.aiAPIKeyValue())
	req.Header.Set("Content-Type", "application/json")
	res, err := s.aiHTTP().Do(req)
	if err != nil {
		return generatedBrief{}, fmt.Errorf("OpenAI request failed: %w", err)
	}
	defer res.Body.Close()
	responseRaw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return generatedBrief{}, errors.New("could not read OpenAI response")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(responseRaw, &apiErr)
		msg := strings.TrimSpace(apiErr.Error.Message)
		if msg == "" {
			msg = http.StatusText(res.StatusCode)
		}
		return generatedBrief{}, fmt.Errorf("OpenAI request failed (%d): %s", res.StatusCode, msg)
	}
	var response openAIResponse
	if err := json.Unmarshal(responseRaw, &response); err != nil {
		return generatedBrief{}, errors.New("OpenAI returned an unreadable response")
	}
	if response.Error != nil && response.Error.Message != "" {
		return generatedBrief{}, errors.New("OpenAI response failed: " + response.Error.Message)
	}
	text, refusal := responseText(response)
	if refusal != "" {
		return generatedBrief{}, errors.New("OpenAI declined the analysis: " + refusal)
	}
	if text == "" {
		return generatedBrief{}, errors.New("OpenAI returned no structured analysis")
	}
	var analysis investigationBrief
	if err := json.Unmarshal([]byte(text), &analysis); err != nil {
		return generatedBrief{}, errors.New("OpenAI response did not match the investigation brief contract")
	}
	if strings.TrimSpace(analysis.ExecutiveSummary) == "" {
		return generatedBrief{}, errors.New("OpenAI response omitted the executive summary")
	}
	model := response.Model
	if model == "" {
		model = s.aiModel()
	}
	return generatedBrief{Analysis: analysis, Model: model, ResponseID: response.ID}, nil
}

func responseText(response openAIResponse) (string, string) {
	var textParts []string
	var refusal string
	for _, output := range response.Output {
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			switch content.Type {
			case "output_text":
				if strings.TrimSpace(content.Text) != "" {
					textParts = append(textParts, content.Text)
				}
			case "refusal":
				if content.Refusal != "" {
					refusal = content.Refusal
				} else {
					refusal = content.Text
				}
			}
		}
	}
	return strings.Join(textParts, "\n"), refusal
}

func (s *server) safetyIdentifier(actorID int64) string {
	raw := fmt.Sprintf("casebook|%s|%d", getMeta(s.db, "team_name"), actorID)
	sum := sha256.Sum256([]byte(raw))
	return "cb_" + hex.EncodeToString(sum[:12])
}

func valueOrMissing(v string) string {
	if strings.TrimSpace(v) == "" {
		return "[not provided]"
	}
	return v
}

const investigationInstructions = `You are an evidence-grounded compliance investigation analyst.

Build a reviewable investigation brief using only the supplied case packet. Do not give legal advice and do not state that misconduct occurred. Treat every conclusion as a proposal for a human investigator.

Evidence rules:
- Every factual allegation, timeline event, policy match, conflict, or risk flag must include one or more short verbatim quotes from the supplied sources.
- Set source to exactly report, policy, or evidence.
- Never invent a quote. If the packet does not support a point, put it under evidence_gaps or review_questions with an empty evidence array.
- Distinguish reported claims from verified facts.
- Use confidence to express source support, not the seriousness of the allegation.

Output rules:
- Keep each item concise and specific.
- IDs must be lowercase letters, numbers, hyphens, or underscores and unique across the whole brief.
- Priority is operational urgency; use low, medium, or high.
- Do not recommend disciplinary action or make a final legal conclusion.
- Recommended actions must be concrete investigation steps that a human can accept or reject.`

func briefJSONSchema() map[string]any {
	citation := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"source": map[string]any{"type": "string", "enum": []string{"report", "policy", "evidence"}},
			"quote":  map[string]any{"type": "string"},
		},
		"required": []string{"source", "quote"},
	}
	item := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"id":         map[string]any{"type": "string"},
			"title":      map[string]any{"type": "string"},
			"detail":     map[string]any{"type": "string"},
			"priority":   map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}},
			"confidence": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}},
			"evidence":   map[string]any{"type": "array", "items": citation},
		},
		"required": []string{"id", "title", "detail", "priority", "confidence", "evidence"},
	}
	props := map[string]any{
		"executive_summary": map[string]any{"type": "string"},
		"scope_note":        map[string]any{"type": "string"},
	}
	for _, name := range []string{"allegations", "timeline", "policy_matches", "evidence_gaps", "conflicts", "risk_flags", "recommended_actions", "review_questions"} {
		props[name] = map[string]any{"type": "array", "items": item}
	}
	return map[string]any{
		"type": "object", "additionalProperties": false, "properties": props,
		"required": []string{"executive_summary", "scope_note", "allegations", "timeline", "policy_matches", "evidence_gaps", "conflicts", "risk_flags", "recommended_actions", "review_questions"},
	}
}

func normalizeAndGroundBrief(brief *investigationBrief, inputs briefInputs) (int, int) {
	seen := map[string]bool{}
	sources := map[string]string{"report": normalizeCitationText(inputs.Report), "policy": normalizeCitationText(inputs.Policy), "evidence": normalizeCitationText(inputs.Evidence)}
	citations, grounded := 0, 0
	process := func(kind string, items []briefItem) []briefItem {
		for i := range items {
			id := strings.ToLower(strings.TrimSpace(items[i].ID))
			if !briefIDPattern.MatchString(id) || seen[id] {
				id = fmt.Sprintf("%s-%02d", strings.TrimSuffix(kind, "s"), i+1)
			}
			for seen[id] {
				id += "x"
			}
			seen[id] = true
			items[i].ID = id
			items[i].Title = strings.TrimSpace(items[i].Title)
			items[i].Detail = strings.TrimSpace(items[i].Detail)
			if !oneOf(items[i].Priority, "low", "medium", "high") {
				items[i].Priority = "medium"
			}
			if !oneOf(items[i].Confidence, "low", "medium", "high") {
				items[i].Confidence = "low"
			}
			if items[i].Evidence == nil {
				items[i].Evidence = []briefCitation{}
			}
			for j := range items[i].Evidence {
				items[i].Evidence[j].Source = strings.ToLower(strings.TrimSpace(items[i].Evidence[j].Source))
				items[i].Evidence[j].Quote = strings.TrimSpace(items[i].Evidence[j].Quote)
				citations++
				normQuote := normalizeCitationText(items[i].Evidence[j].Quote)
				items[i].Evidence[j].Grounded = normQuote != "" && strings.Contains(sources[items[i].Evidence[j].Source], normQuote)
				if items[i].Evidence[j].Grounded {
					grounded++
				}
			}
		}
		return items
	}
	brief.Allegations = process("allegations", brief.Allegations)
	brief.Timeline = process("timeline", brief.Timeline)
	brief.PolicyMatches = process("policy_matches", brief.PolicyMatches)
	brief.EvidenceGaps = process("evidence_gaps", brief.EvidenceGaps)
	brief.Conflicts = process("conflicts", brief.Conflicts)
	brief.RiskFlags = process("risk_flags", brief.RiskFlags)
	brief.RecommendedActions = process("recommended_actions", brief.RecommendedActions)
	brief.ReviewQuestions = process("review_questions", brief.ReviewQuestions)
	return citations, grounded
}

func normalizeCitationText(v string) string {
	return strings.ToLower(strings.Join(strings.Fields(v), " "))
}
func oneOf(v string, allowed ...string) bool {
	for _, candidate := range allowed {
		if v == candidate {
			return true
		}
	}
	return false
}

func (s *server) aiBriefsForCase(caseID int64) ([]aiBriefRecord, error) {
	rows, err := s.db.Query(`SELECT b.id,b.created_at,COALESCE(m.name,'Unknown'),b.model,b.response_id,b.status,b.input_sha256,b.analysis_json,b.citation_count,b.grounded_citations
FROM ai_briefs b LEFT JOIN members m ON m.id=b.created_by WHERE b.case_id=? ORDER BY b.id DESC`, caseID)
	if err != nil {
		return nil, err
	}
	briefs := []aiBriefRecord{}
	for rows.Next() {
		var rec aiBriefRecord
		var analysisJSON string
		if err := rows.Scan(&rec.ID, &rec.CreatedAt, &rec.CreatedBy, &rec.Model, &rec.ResponseID, &rec.Status, &rec.InputSHA256, &analysisJSON, &rec.CitationCount, &rec.GroundedCitations); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(analysisJSON), &rec.Analysis); err != nil {
			rows.Close()
			return nil, err
		}
		briefs = append(briefs, rec)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// openStore intentionally allows one SQLite connection. Load child rows only
	// after closing the parent cursor so this remains deadlock-free.
	for i := range briefs {
		briefs[i].Decisions, err = s.aiDecisions(briefs[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return briefs, nil
}

func (s *server) aiBriefByID(caseID, briefID int64) (aiBriefRecord, error) {
	briefs, err := s.aiBriefsForCase(caseID)
	if err != nil {
		return aiBriefRecord{}, err
	}
	for _, brief := range briefs {
		if brief.ID == briefID {
			return brief, nil
		}
	}
	return aiBriefRecord{}, sql.ErrNoRows
}

func (s *server) aiDecisions(briefID int64) ([]aiBriefDecision, error) {
	rows, err := s.db.Query(`SELECT d.item_kind,d.item_id,d.item_title,d.decision,d.decided_at,COALESCE(m.name,'Unknown'),d.applied_to
FROM ai_brief_decisions d LEFT JOIN members m ON m.id=d.decided_by WHERE d.brief_id=? ORDER BY d.id`, briefID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []aiBriefDecision{}
	for rows.Next() {
		var d aiBriefDecision
		if err := rows.Scan(&d.ItemKind, &d.ItemID, &d.ItemTitle, &d.Decision, &d.DecidedAt, &d.DecidedBy, &d.AppliedTo); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type locatedBriefItem struct {
	Kind string
	Item briefItem
}

func briefItemsByID(brief investigationBrief) map[string]locatedBriefItem {
	out := map[string]locatedBriefItem{}
	add := func(kind string, items []briefItem) {
		for _, item := range items {
			out[item.ID] = locatedBriefItem{Kind: kind, Item: item}
		}
	}
	add("allegations", brief.Allegations)
	add("timeline", brief.Timeline)
	add("policy_matches", brief.PolicyMatches)
	add("evidence_gaps", brief.EvidenceGaps)
	add("conflicts", brief.Conflicts)
	add("risk_flags", brief.RiskFlags)
	add("recommended_actions", brief.RecommendedActions)
	add("review_questions", brief.ReviewQuestions)
	return out
}

func (s *server) handleDecideAIBrief(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	c, err := s.caseByNo(r.PathValue("no"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "case not found")
		return
	}
	if !s.canEdit(c, actor) {
		writeErr(w, http.StatusForbidden, "only the owner or active coverer can review an AI brief")
		return
	}
	briefID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid brief id")
		return
	}
	brief, err := s.aiBriefByID(c.ID, briefID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "AI brief not found")
		return
	}
	var req struct {
		Decisions []struct {
			ItemID   string `json:"item_id"`
			Decision string `json:"decision"`
		} `json:"decisions"`
	}
	if err := readJSON(r, &req); err != nil || len(req.Decisions) == 0 {
		writeErr(w, http.StatusBadRequest, "at least one decision is required")
		return
	}
	items := briefItemsByID(brief.Analysis)
	seen := map[string]bool{}
	for i := range req.Decisions {
		req.Decisions[i].ItemID = strings.TrimSpace(req.Decisions[i].ItemID)
		req.Decisions[i].Decision = strings.ToUpper(strings.TrimSpace(req.Decisions[i].Decision))
		if seen[req.Decisions[i].ItemID] {
			writeErr(w, http.StatusBadRequest, "duplicate item decision")
			return
		}
		seen[req.Decisions[i].ItemID] = true
		if _, ok := items[req.Decisions[i].ItemID]; !ok {
			writeErr(w, http.StatusBadRequest, "unknown AI brief item: "+req.Decisions[i].ItemID)
			return
		}
		if !oneOf(req.Decisions[i].Decision, "ACCEPTED", "REJECTED") {
			writeErr(w, http.StatusBadRequest, "decision must be ACCEPTED or REJECTED")
			return
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	for _, decision := range req.Decisions {
		located := items[decision.ItemID]
		var prior string
		err := tx.QueryRow(`SELECT decision FROM ai_brief_decisions WHERE brief_id=? AND item_id=?`, briefID, decision.ItemID).Scan(&prior)
		if err == nil {
			if prior != decision.Decision {
				writeErr(w, http.StatusConflict, "this item already has an immutable review decision")
				return
			}
			continue
		}
		if err != sql.ErrNoRows {
			writeErr(w, http.StatusInternalServerError, "could not check prior decision")
			return
		}

		appliedTo := "audit_only"
		checklistAdded := false
		checklistLabel := ""
		if decision.Decision == "ACCEPTED" && (located.Kind == "recommended_actions" || located.Kind == "evidence_gaps") {
			checklistLabel = located.Item.Title
			if located.Kind == "evidence_gaps" {
				checklistLabel = "Obtain evidence: " + checklistLabel
			}
			var existingID int64
			err := tx.QueryRow(`SELECT id FROM checklist_items WHERE case_id=? AND lower(label)=lower(?) LIMIT 1`, c.ID, checklistLabel).Scan(&existingID)
			if err == sql.ErrNoRows {
				_, err = tx.Exec(`INSERT INTO checklist_items(case_id,label,state,sort,created_at)
VALUES(?,?,'Needed',COALESCE((SELECT MAX(sort)+1 FROM checklist_items WHERE case_id=?),0),?)`, c.ID, checklistLabel, c.ID, nowStamp())
				if err != nil {
					writeErr(w, http.StatusInternalServerError, "could not apply AI item to checklist")
					return
				}
				checklistAdded = true
				appliedTo = "checklist"
			} else if err == nil {
				appliedTo = "existing_checklist"
			} else {
				writeErr(w, http.StatusInternalServerError, "could not inspect checklist")
				return
			}
		}
		_, err = tx.Exec(`INSERT INTO ai_brief_decisions(brief_id,item_kind,item_id,item_title,decision,decided_at,decided_by,applied_to)
VALUES(?,?,?,?,?,?,?,?)`, briefID, located.Kind, decision.ItemID, located.Item.Title, decision.Decision, nowStamp(), actor.ID, appliedTo)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "could not store AI review decision")
			return
		}
		eventKind := "AI_REJECTED"
		if decision.Decision == "ACCEPTED" {
			eventKind = "AI_ACCEPTED"
		}
		if err := addEvent(tx, c.ID, actor.ID, eventKind, map[string]any{
			"brief_id": briefID, "item_id": decision.ItemID, "item_kind": located.Kind,
			"title": located.Item.Title, "detail": located.Item.Detail, "model": brief.Model, "applied_to": appliedTo,
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, "could not append AI review audit event")
			return
		}
		if checklistAdded {
			if err := addEvent(tx, c.ID, actor.ID, "CHECKLIST", map[string]any{"label": checklistLabel, "action": "added", "source": "ai_brief", "brief_id": briefID}); err != nil {
				writeErr(w, http.StatusInternalServerError, "could not append checklist audit event")
				return
			}
		}
	}
	if _, err := tx.Exec(`UPDATE ai_briefs SET status='REVIEWED' WHERE id=?`, briefID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, err := s.aiBriefByID(c.ID, briefID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "review saved but brief could not be reloaded")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"brief": updated})
}

func demoBriefInputs() briefInputs {
	return briefInputs{
		Report:   `On July 8, 2026, Jordan Lee reported that Regional Manager Morgan Vale directed staff to backdate three customer suitability reviews. Jordan said the instruction was given during the July 7 morning huddle. Jordan did not identify the three customer accounts.`,
		Policy:   `Policy 7.2 requires suitability reviews to be completed and dated on the day the review occurs. Policy 2.1 prohibits altering books and records to create a misleading record. Potential books-and-records issues must be escalated to Compliance within two business days.`,
		Evidence: `Email dated July 9 from Morgan Vale to the branch team states: "Please make sure the outstanding reviews show the original June completion dates before Friday." The review system export shows five records edited on July 9, but the export does not identify the user who made each edit.`,
	}
}

func demoInvestigationBrief() investigationBrief {
	return investigationBrief{
		ExecutiveSummary: "A staff report and follow-up email raise a potential books-and-records concern involving backdated suitability reviews. The supplied material supports opening a scoped investigation, but it does not establish who changed the records or which customer accounts were affected.",
		ScopeNote:        "This is an investigation aid, not a finding of misconduct or legal advice. Every proposal requires human review.",
		Allegations:      []briefItem{{ID: "allegation-backdating", Title: "Direction to backdate suitability reviews", Detail: "Jordan Lee reported that a regional manager instructed staff to change review dates. This is a reported allegation, not a verified fact.", Priority: "high", Confidence: "medium", Evidence: []briefCitation{{Source: "report", Quote: "directed staff to backdate three customer suitability reviews"}}}},
		Timeline: []briefItem{
			{ID: "timeline-huddle", Title: "July 7 — alleged verbal instruction", Detail: "The report places the alleged instruction at a morning huddle.", Priority: "medium", Confidence: "medium", Evidence: []briefCitation{{Source: "report", Quote: "the instruction was given during the July 7 morning huddle"}}},
			{ID: "timeline-email", Title: "July 9 — follow-up email", Detail: "An email asked that outstanding reviews show original June completion dates.", Priority: "high", Confidence: "high", Evidence: []briefCitation{{Source: "evidence", Quote: "Please make sure the outstanding reviews show the original June completion dates before Friday."}}},
		},
		PolicyMatches: []briefItem{
			{ID: "policy-review-date", Title: "Policy 7.2 — contemporaneous dating", Detail: "Changing review dates may conflict with the requirement to date a review when it occurs.", Priority: "high", Confidence: "high", Evidence: []briefCitation{{Source: "policy", Quote: "suitability reviews to be completed and dated on the day the review occurs"}}},
			{ID: "policy-escalation", Title: "Two-business-day escalation", Detail: "The potential records issue may trigger internal escalation timing.", Priority: "high", Confidence: "high", Evidence: []briefCitation{{Source: "policy", Quote: "must be escalated to Compliance within two business days"}}},
		},
		EvidenceGaps: []briefItem{
			{ID: "gap-account-list", Title: "Identify the affected customer accounts", Detail: "The report does not name the three accounts referenced by the reporter.", Priority: "high", Confidence: "high", Evidence: []briefCitation{}},
			{ID: "gap-audit-log", Title: "Obtain the review-system user audit log", Detail: "The current export shows edits but does not attribute them to a user.", Priority: "high", Confidence: "high", Evidence: []briefCitation{{Source: "evidence", Quote: "the export does not identify the user who made each edit"}}},
		},
		Conflicts: []briefItem{{ID: "conflict-count", Title: "Three reported reviews versus five edited records", Detail: "The reporter referenced three reviews, while the system export shows five edited records. Scope should be reconciled before conclusions are drawn.", Priority: "medium", Confidence: "high", Evidence: []briefCitation{{Source: "report", Quote: "three customer suitability reviews"}, {Source: "evidence", Quote: "five records edited on July 9"}}}},
		RiskFlags: []briefItem{{ID: "risk-record-integrity", Title: "Potential books-and-records integrity risk", Detail: "The supplied instruction and email could indicate an attempt to create inaccurate completion dates.", Priority: "high", Confidence: "medium", Evidence: []briefCitation{{Source: "policy", Quote: "prohibits altering books and records to create a misleading record"}, {Source: "evidence", Quote: "show the original June completion dates"}}}},
		RecommendedActions: []briefItem{
			{ID: "action-preserve", Title: "Preserve the review-system audit logs and July 9 email", Detail: "Issue a targeted preservation step before logs rotate or messages are altered.", Priority: "high", Confidence: "high", Evidence: []briefCitation{{Source: "evidence", Quote: "Email dated July 9 from Morgan Vale to the branch team"}}},
			{ID: "action-interview-reporter", Title: "Interview Jordan Lee about the July 7 huddle", Detail: "Confirm who attended, the words used, and the three accounts referenced.", Priority: "medium", Confidence: "medium", Evidence: []briefCitation{{Source: "report", Quote: "the instruction was given during the July 7 morning huddle"}}},
		},
		ReviewQuestions: []briefItem{{ID: "question-authority", Title: "Who had authority to edit the five records?", Detail: "Identify users, timestamps, prior values, and new values for each edit.", Priority: "high", Confidence: "high", Evidence: []briefCitation{}}},
	}
}
