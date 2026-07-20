# Devpost submission copy

Final submission copy for OpenAI Build Week 2026.

## Project name

Casebook AI

## Tagline

Turn messy compliance reports, policies, and evidence into an auditable investigation workflow.

## Track

Work & Productivity

## One-line description

An evidence-grounded compliance case copilot that proposes a cited investigation brief, verifies source quotes, and applies only the actions a human investigator approves.

## Inspiration

Compliance investigations rarely arrive as clean data. A reviewer may receive a report, policy passages, email excerpts, a partial system export, and a deadline—then spend hours building a timeline, identifying missing evidence, and documenting why each next step was chosen. Generic AI summaries are not enough because regulated work needs provenance, human judgment, and an audit trail.

I had already built Casebook as a local case-management tool for small compliance teams. For OpenAI Build Week, I extended it with a narrow AI workflow that addresses the hardest part of an investigation without pretending the model is the decision-maker.

## What it does

The investigator opens a case, pastes an approved report, policy excerpt, and evidence notes, and explicitly submits that packet for analysis. GPT-5.6-sol produces a structured brief containing:

- allegations distinguished from verified facts
- a dated timeline
- policy matches
- evidence gaps
- source conflicts
- risk flags
- recommended investigation actions
- review and interview questions

Factual proposals must include short source quotes. Casebook independently checks each quote against the submitted text and displays whether it is an exact match. The case owner then accepts or rejects proposals one by one. Accepted evidence gaps and recommended actions become ordinary checklist items. Every generation and review decision is appended to the case timeline.

The model cannot silently edit the case or make a final misconduct or legal determination.

## How I built it

Casebook is a portable Go application with an embedded HTML/CSS/JavaScript interface and a local SQLite database. The Build Week extension uses the OpenAI Responses API with GPT-5.6-sol, medium reasoning effort, and strict JSON Schema output.

The server adds several safeguards around the model call:

- explicit user submission and cloud-processing notice
- `store: false`
- only text pasted into the AI Brief tab plus minimal case context
- input limits and a SHA-256 packet fingerprint
- per-user hashed safety identifier
- exact-quote grounding performed after generation
- owner/coverer permission checks
- immutable, idempotent review decisions
- append-only provenance events

I also included a deterministic synthetic fixture so judges can exercise the full review, checklist, and audit workflow without an API key.

## Challenges

The main challenge was designing useful model assistance without weakening the case record. A polished summary would have been easy, but it would not be defensible. I had to separate model proposals from human decisions, require evidence for factual items, verify those quotes outside the model, and make accepted actions flow into the same operational checklist and audit trail as ordinary case work.

Another challenge was maintaining a clear privacy boundary in a local-first application. The UI therefore makes the cloud step explicit and sends only the packet the investigator chooses to paste.

## Accomplishments that I am proud of

- The AI output is operational, not decorative: accepted actions become trackable work.
- Every factual citation is independently checked against the submitted packet.
- Human review is item-level and permanent; the AI never writes silently.
- The entire flow works with a deterministic synthetic demo.
- The live GPT-5.6-sol test returned 32 citations, all 32 verified against the fictional source packet.
- The pre-existing application and Build Week additions are separated by a tagged baseline and dated commits.

## What I learned

The most valuable AI pattern for regulated work is not “answer the question.” It is “propose a structured, sourced intermediate artifact that a responsible human can inspect and apply.” Strict schemas make the interface predictable, but product trust comes from the surrounding controls: data minimization, independent grounding, permissions, immutable decisions, and visible provenance.

## What's next

- File extraction with local redaction before optional model submission
- Policy-library versioning and effective-date checks
- Conflict review across interviews and evidence packets
- Configurable retention and encryption for AI inputs
- Enterprise authentication, TLS, and role-based access control
- Evaluation sets for citation coverage, unsupported claims, and investigator usefulness

## Built with

Go, SQLite, HTML, CSS, JavaScript, OpenAI Responses API, GPT-5.6-sol, JSON Schema, Codex

## Links

- Repository: https://github.com/CCCCCRH0405/casebook-ai
- Demo video: https://youtu.be/km7yKSPvfw0
- Downloadable release: https://github.com/CCCCCRH0405/casebook-ai/releases/tag/v0.2.0-buildweek
- Codex session ID from `/feedback`: provided directly in the private Devpost submission form

## Judge instructions

1. Download the release for macOS, Windows, or Linux. Alternatively, clone the repository and run `go build -o casebook .` with Go 1.24+.
2. Start the downloaded `casebook` executable (or run `./casebook` from the repository).
3. Complete the local setup wizard and create or open a sample case.
4. Open the case's **AI Brief** tab.
5. Click **Load synthetic demo**, then **Generate synthetic brief**.
6. Inspect the exact-source-match badges.
7. Accept one recommended action.
8. Confirm the action appears under **Checklist** and the provenance appears under **Activity**.

No API key is required for this judge path. To test the live model path, start Casebook with `OPENAI_API_KEY` set and submit only synthetic data.
