# Casebook — Manual QA Checklist

**Release rule:** every interactive element must produce visible feedback — success toast, "nothing to do" message, or error toast. Silent failure = release blocker.

---

## 1. Setup Wizard (`/` on first run, `setup_done = false`)

- [ ] **Create workspace — happy path:** fill team name + your name, click "Create workspace" → success toast "Workspace ready", shell renders
- [ ] **Create workspace — empty team name:** leave team name blank, click button → error toast "team name and your name are required"
- [ ] **Create workspace — empty your name:** fill team name only, click button → error toast
- [ ] **Create workspace — with teammates:** add two names in textarea, submit → workspace created, both teammates visible on Who screen
- [ ] **Create workspace — duplicate click:** submit then quickly click again while request is in-flight → no duplicate workspace

---

## 2. Who-Are-You / PIN Sign-In

- [ ] **Pick member without PIN:** click name button → immediately signed in, shell renders, no extra prompt
- [ ] **Pick member with PIN — correct PIN:** click name, enter correct PIN, click "Sign in" → signed in, shell renders
- [ ] **Pick member with PIN — wrong PIN:** enter wrong PIN, click "Sign in" → error toast (server error message)
- [ ] **Pick member with PIN — Enter key submits:** type PIN, press Enter → same as clicking "Sign in"
- [ ] **Pick member with PIN — empty PIN field:** click "Sign in" with blank PIN field → error toast (server rejects)
- [ ] **Avatar button (switch user):** click initials button top-right → returns to Who screen, identity cleared

---

## 3. Top Nav + Quick-Add Bar

- [ ] **Quick-add — create case:** type a title, press Enter → toast "[CASE-NO] created", cases list refreshes, input clears
- [ ] **Quick-add — blank Enter:** press Enter with empty input → no request sent, no toast, no error
- [ ] **Quick-add — double Enter:** submit then immediately press Enter again while first is in-flight → input disabled during request, no duplicate
- [ ] **Quick-add — server error:** simulate server down → error toast with message
- [ ] **`/` keyboard shortcut:** press `/` while focus is not on an input → quick-add input gains focus
- [ ] **Nav links:** click Today / Cases / Recurring / Inbox / Settings → active link highlighted (`on` class), correct view loads
- [ ] **Inbox badge:** when pending requests exist, badge count is visible on Inbox link; clears when count is 0

---

## 4. Today View

- [ ] **Row click:** click any case row → navigates to `#/case/<no>`
- [ ] **Stat line — unassigned link:** when unassigned count > 0, click the unassigned link → navigates to Cases filtered by unassigned owner
- [ ] **Empty sections:** when no cases are due today/this week/waiting → "Nothing due today." / "Nothing due this week." / "Nothing is blocked..." messages shown (not blank)

---

## 5. Cases List + Filters

- [ ] **Search (debounced):** type in search box → table updates after ~250 ms, no toast needed
- [ ] **Status filter:** change dropdown → table updates immediately
- [ ] **Owner filter:** change to a specific member → table shows only their cases
- [ ] **Owner filter — unassigned:** select "Unassigned" → shows only cases with no owner
- [ ] **Type filter:** change dropdown → table updates
- [ ] **View filter — Active / Archived / Everything:** switch values → table updates
- [ ] **Combined filters — no results:** set filters that match nothing → "No cases match." message (not blank table)
- [ ] **Filter error:** if API fails → error toast
- [ ] **Row click:** click row → navigates to case detail

---

## 6. Case Detail

### Fields (auto-save on blur/change + Save button)

- [ ] **Title — edit and blur:** change title, click away → toast "Saved"
- [ ] **Title — clear and blur:** delete all text, click away → error toast "title cannot be empty", field reverts to original
- [ ] **Type dropdown — change:** select different type → toast "Saved"
- [ ] **Owner dropdown — change:** select different owner → toast "Saved", page reloads (owner display updates)
- [ ] **Due date — change:** pick a date → toast "Saved"
- [ ] **Hard deadline — change:** pick a date → toast "Saved"
- [ ] **Source — edit and blur:** change text, tab out → toast "Saved"
- [ ] **Workpaper location — edit and blur:** change text, tab out → toast "Saved"
- [ ] **Tags — edit and blur:** add comma-separated tags, tab out → toast "Saved"
- [ ] **Description — edit and blur:** change textarea, click away → toast "Saved"
- [ ] **Save button — with changes:** change multiple fields, click Save → toast "Saved"
- [ ] **Save button — no changes:** click Save without editing anything → toast "All changes already saved"
- [ ] **Save button — double-click:** click Save twice quickly → button disabled during request, no duplicate save

### Status Buttons

- [ ] **Start (→ In Progress):** click → toast "Moved to In Progress", status dot updates
- [ ] **Complete:** click → toast "Completed — auto-archives in 7 days", status updates
- [ ] **Back to open (→ Open):** click → toast "Moved to Open"
- [ ] **Reopen (from Completed/Archived/Cancelled):** click → toast "Moved to In Progress" or appropriate status
- [ ] **Archive now:** click → toast "Moved to Archived"

### Waiting Inline Form

- [ ] **Mark waiting… — submit:** click "Mark waiting…", select waiting-on option, optionally fill detail, click "Mark waiting" → case status updates to Waiting, waiting info visible in status line
- [ ] **Mark waiting… — never mind:** click "Never mind" → inline form disappears, no change
- [ ] **Mark waiting… — server error:** simulate error → error toast

### Cancel Inline Form

- [ ] **Cancel… — submit with reason:** click "Cancel…", type reason, click "Cancel case" → case moves to Cancelled
- [ ] **Cancel… — submit without reason:** leave reason blank, click "Cancel case" → accepted (reason optional), case cancelled
- [ ] **Cancel… — never mind:** click "Never mind" → form disappears, no change

### Claim

- [ ] **Claim this case (unassigned case):** click "Claim this case" → toast "It's yours", owner field updates to current user

### Coverage / Transfer / Help Request Forms

- [ ] **Ask someone to cover — send:** click "Ask someone to cover…", select member, optionally add message, click "Send" → toast "Request sent", request appears in requests block
- [ ] **Ask someone to cover — never mind:** click "Never mind" → form disappears
- [ ] **Request transfer — send:** click "Request transfer…", add message, click "Send" → toast "Request sent"
- [ ] **Request transfer — never mind:** click "Never mind" → form disappears
- [ ] **Ask for help — send:** click "Ask for help…", select member, add message, click "Send" → toast "Request sent"
- [ ] **Ask for help — no teammates:** if only one active member exists, recipient list is empty → server returns error, toast shown
- [ ] **Any request — server error:** simulate error → error toast

### Request Accept / Decline / Withdraw (on case detail)

- [ ] **Accept request:** click Accept → toast "Accepted", request removed from list
- [ ] **Decline request:** click Decline → toast "Declined", request removed from list
- [ ] **Withdraw request:** click Withdraw → toast "Withdrawn", request removed from list

### End Coverage

- [ ] **End coverage:** when coverer is shown, click "End coverage" → toast "Coverage ended", coverer name disappears

### Make Recurring Form

- [ ] **Make recurring — submit:** click "Make recurring…", select frequency, verify/change due date and lead days, click "Make recurring" → toast "Recurring item created — see the Recurring tab", case header shows "↻ recurring"
- [ ] **Make recurring — frequency change updates default date:** change frequency dropdown → due date field updates automatically
- [ ] **Make recurring — never mind:** click "Never mind" → form disappears
- [ ] **Make recurring — server error:** simulate error → error toast

### Activity / Checklist Tabs

- [ ] **Switch to Checklist tab:** click "Checklist" → checklist pane visible, activity pane hidden
- [ ] **Switch to Activity tab:** click "Activity" → activity pane visible, checklist pane hidden
- [ ] **Tab selection persists across navigation:** navigate away and back → same tab is shown

### Checklist

- [ ] **Add item — button:** type item name in input, click "Add" → item appears in list (reload)
- [ ] **Add item — Enter key:** type item name, press Enter → item appears in list
- [ ] **Add item — empty input:** click "Add" or press Enter with blank input → no request sent, no toast, no change
- [ ] **Advance state (Needed → Requested → Received → N/A → Needed):** click state pill → pill updates to next state
- [ ] **Delete item:** click × button → item removed from list
- [ ] **Checklist — read-only for non-owner/non-coverer:** state pills and × buttons not rendered, add form not rendered
- [ ] **Checklist — server error on any action:** error toast

### Note Composer

- [ ] **Add note — with text:** type in note textarea, click "Add note" → note appears in activity feed at top
- [ ] **Add note — empty textarea:** click "Add note" with blank input → no request sent, no toast, no change
- [ ] **Add note — server error:** simulate error → error toast

---

## 7. Recurring Page

### List

- [ ] **Empty state:** no recurring items → "Nothing recurring yet. Add one below…" message shown
- [ ] **Paused item display:** paused items shown at reduced opacity with "· paused" label

### Create Now

- [ ] **Create now button:** click "Create now" on active item → toast "[CASE-NO] created", list refreshes
- [ ] **Create now — server error:** simulate error → error toast
- [ ] **Create now button not shown for paused items:** verify button is absent when `o.active = false`

### Pause / Resume

- [ ] **Pause:** click "Pause" on active item → item shows as paused (opacity, label), button changes to "Resume"
- [ ] **Resume:** click "Resume" on paused item → item becomes active, button changes to "Pause"
- [ ] **Pause/Resume — server error:** error toast

### New Recurring Item Form

- [ ] **Add recurring item — happy path:** fill name, select type/owner/frequency/due date/lead days, optionally add checklist lines, click "Add recurring item" → toast "Recurring item added", item appears in list
- [ ] **Add recurring item — name empty:** leave name blank, click "Add" → server rejects (name required), error toast
- [ ] **Add recurring item — no due date:** leave date blank, click "Add" → server error toast (or accepted if optional, verify behavior)
- [ ] **Add recurring item — server error:** error toast

---

## 8. Inbox

### Needs Your Decision

- [ ] **Accept:** click "Accept" → toast "Accepted", item moves to Recently Resolved, badge decrements
- [ ] **Decline:** click "Decline" → toast "Declined", item moves to Recently Resolved, badge decrements
- [ ] **Accept/Decline — server error:** error toast

### Sent by You, Still Open

- [ ] **Withdraw:** click "Withdraw" → toast "Withdrawn", item moves to Recently Resolved
- [ ] **Withdraw — server error:** error toast

### Recently Resolved

- [ ] **Resolved items — no action buttons:** only status label shown (accepted/declined), no buttons rendered

### Empty States

- [ ] **All sections empty:** each section shows its empty message ("Nothing waiting on you." / "No open requests from you." / "No history yet.")

### Case link

- [ ] **Case number link:** click `[CASE-NO]` link in any inbox item → navigates to that case detail

---

## 9. Settings

### Members

- [ ] **Add member — happy path:** type name, click "Add member" → toast "[Name] added — they can now pick their name on the join screen", member appears in list
- [ ] **Add member — empty name:** click "Add member" with blank input → no request sent, no toast, no change (guard in JS)
- [ ] **Add member — server error:** error toast
- [ ] **Deactivate member:** click "Deactivate" → button changes to "Reactivate", member label shows "· deactivated"
- [ ] **Reactivate member:** click "Reactivate" → button changes back to "Deactivate", deactivated label gone
- [ ] **Deactivate/Reactivate — server error:** error toast

### PIN — Self

- [ ] **Set PIN (no existing PIN):** click "Set PIN", enter new PIN, click "Save PIN" → toast "PIN saved", button changes to "Change PIN", "Remove PIN" appears
- [ ] **Change PIN (existing PIN):** click "Change PIN", enter current PIN, enter new PIN, click "Save PIN" → toast "PIN saved"
- [ ] **Change PIN — wrong current PIN:** enter wrong current PIN → error toast (server rejects)
- [ ] **Change PIN — new PIN empty:** leave new PIN blank → server rejects (too short), error toast
- [ ] **Remove PIN:** click "Remove PIN", enter current PIN, click "Remove PIN" (danger button) → toast "PIN removed", button reverts to "Set PIN"
- [ ] **Remove PIN — wrong current PIN:** enter wrong PIN → error toast
- [ ] **PIN form — never mind:** click "Never mind" → form disappears, no change

### PIN — Admin (Clear another member's PIN)

- [ ] **Clear PIN (admin only, other member):** click "Clear PIN" next to member → toast "PIN removed", "· PIN set" label disappears for that member
- [ ] **Clear PIN — not visible to non-admin:** verify "Clear PIN" button is not rendered for non-admin users

### Case Types

- [ ] **Add type — happy path:** type name, click "Add type" → type appears in list (no explicit toast in current code — verify server returns and list re-renders; flag if silent)
- [ ] **Add type — empty name:** click "Add type" with blank input → no request sent (JS guard), no change
- [ ] **Add type — server error:** error toast

---

## 10. AI Brief

### Data boundary and generation

- [x] **No-key state:** without `OPENAI_API_KEY`, the tab says the live model is not configured and the synthetic demo remains available
- [x] **Synthetic fixture:** click "Load synthetic demo" → fictional report, policy, and evidence are populated; the generated brief is labeled `demo-fixture`
- [x] **Consent gate:** live analysis is blocked with a visible message until the cloud-processing checkbox is selected
- [x] **Live GPT-5.6-sol run:** submit only the synthetic packet → structured brief renders successfully
- [x] **Input contract:** mock API test confirms model, `store: false`, medium reasoning, safety identifier, and strict JSON schema
- [x] **Permission check:** a member who is neither owner nor active coverer cannot generate a brief
- [x] **Missing report:** empty required report is rejected with a visible error
- [x] **Oversized inputs:** per-source and total source limits are enforced server-side

### Grounding and review

- [x] **Citation grounding:** source quotes show exact-match status; synthetic live run verified 32/32 citations
- [x] **Human acceptance:** accept a recommended action → immutable decision is saved
- [x] **Checklist application:** accepted action appears as a Needed checklist item
- [x] **Human rejection:** reject an item → decision is saved but no case field or checklist item changes
- [x] **Idempotency:** repeating the same decision creates no duplicate checklist item
- [x] **Conflicting decision:** attempting to reverse a saved decision is rejected
- [x] **Read-only review:** a non-owner/non-coverer cannot accept or reject items

### Audit trail

- [x] **Generation event:** Activity shows model, brief ID, and grounded-citation count
- [x] **Decision event:** Activity shows accepted/rejected item and application target
- [x] **Checklist provenance:** applied action includes the originating AI brief ID
- [x] **Frontend payload rendering:** structured event payloads render human-readable text rather than `[object Object]`
