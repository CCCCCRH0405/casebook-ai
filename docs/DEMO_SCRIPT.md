# Casebook AI — 2:40 demo script

Record at 1080p. Keep the browser zoom near 100%, use only the included synthetic case, and show the timer while rehearsing. The final video must remain under three minutes.

## 0:00–0:20 — Problem

**On screen:** Today view, then open the synthetic case.

**Voiceover:**

> Compliance investigations start with messy reports, policy excerpts, emails, and incomplete system exports. Teams lose time assembling the facts—and still need every conclusion to be reviewable and auditable. Casebook AI turns that packet into a human-controlled investigation workflow.

## 0:20–0:42 — Existing operational shell

**On screen:** Case fields, Checklist, and Activity tabs.

**Voiceover:**

> Casebook already tracks ownership, deadlines, evidence, recurring obligations, and an append-only case history in a local SQLite workspace. I used Codex to map the existing system, implement and test the new workflow, and run UI QA. GPT-5.6-sol powers the evidence-grounded AI Brief—not a generic chatbot.

## 0:42–1:02 — Explicit data boundary

**On screen:** AI Brief tab. Show the cloud notice and the three source boxes, then click **Load synthetic demo**.

**Voiceover:**

> Nothing runs automatically. The investigator chooses exactly what to submit: a report, policy text, and evidence notes. Only this pasted text is sent to OpenAI, and the request uses storage disabled. This demo is entirely fictional.

## 1:02–1:42 — Structured, grounded analysis

**On screen:** Generate the brief. Scroll through summary, allegations, timeline, policy matches, evidence gaps, conflict, and risk flag. Pause on exact-match badges.

**Voiceover:**

> GPT-5.6-sol returns a strict structured brief: allegations, timeline, policy mapping, missing evidence, conflicts, risks, recommended actions, and interview questions. Factual items must quote the submitted packet. Casebook then verifies every quote locally and marks exact source matches. Unsupported points belong under gaps or questions—not invented facts.

## 1:42–2:15 — Human decision and application

**On screen:** Accept “Preserve the review-system audit logs and July 9 email.” Reject or leave another proposal for later. Open Checklist.

**Voiceover:**

> The model cannot change the case. The owner reviews each proposal. I’ll accept this preservation step. Casebook turns it into a normal checklist item; rejected items remain in the record but do not change operations.

## 2:15–2:35 — Audit trail

**On screen:** Activity tab. Highlight the generated-brief event, accepted proposal, and checklist event.

**Voiceover:**

> Generation, model, grounded-citation count, human decision, and resulting checklist change are all appended to the case timeline. That creates a defensible chain from source, to model proposal, to human action.

## 2:35–2:40 — Close

**On screen:** Casebook logo or AI Brief summary.

**Voiceover:**

> Casebook AI: faster investigations without giving up human judgment or auditability.

## Recording checklist

- Show the `Synthetic demo` label.
- Do not show an API key, terminal environment, real firm name, or real case data.
- Keep the final export under 3:00.
- Upload as a publicly visible YouTube video, as required by the official rules.
- Add the video URL and Codex `/feedback` session ID to Devpost.
