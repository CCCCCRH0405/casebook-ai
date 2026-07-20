# Casebook AI

**Turn messy compliance reports, policies, and evidence into an auditable investigation workflow.**

Casebook is a local-first case tracker for small compliance teams. Its optional AI Brief workflow uses OpenAI GPT-5.6-sol to organize a pasted case packet into a reviewable investigation brief—with exact source quotes, explicit confidence, human accept/reject decisions, and an append-only audit trail.

Built for [OpenAI Build Week 2026](https://openai.devpost.com/).

**[Watch the 2:48 demo](https://youtu.be/km7yKSPvfw0) · [Download v0.2.0 Build Week release](https://github.com/CCCCCRH0405/casebook-ai/releases/tag/v0.2.0-buildweek) · [View the public repository](https://github.com/CCCCCRH0405/casebook-ai)**

## Why this exists

Compliance investigations rarely begin with a neat dataset. They begin with a report, a policy PDF, email excerpts, partial exports, and a deadline. Spreadsheets can track rows, but they do not reliably answer:

- Which claims are supported by which source?
- What evidence is missing or contradictory?
- Which next steps did a human investigator approve?
- What changed, when, and by whom?

Casebook keeps the operational record local while adding an evidence-grounded AI layer that proposes—not decides—the next investigation steps.

## The Build Week workflow

1. Open a case and choose **AI Brief**.
2. Paste a synthetic or approved report, policy excerpt, and evidence notes.
3. Confirm the cloud-processing notice and run the analysis.
4. Review allegations, timeline events, policy matches, evidence gaps, conflicts, risk flags, recommended actions, and interview questions.
5. Inspect every quote. Casebook checks each quoted passage against the submitted source text and marks whether it is an exact match.
6. Accept or reject each proposal. Accepted evidence gaps and actions become checklist items; every decision becomes an immutable case event.

The model never changes the case silently and never makes a final misconduct or legal determination.

## What makes the AI workflow auditable

- **Structured output:** GPT-5.6-sol must satisfy a strict JSON schema.
- **Source grounding:** factual items require short quotes labeled `report`, `policy`, or `evidence`.
- **Server-side verification:** Casebook independently checks whether each quote appears in the submitted source.
- **Human-in-the-loop:** proposals remain inert until the case owner or active coverer accepts or rejects them.
- **Append-only provenance:** generation, acceptance, rejection, and checklist application are recorded in the case timeline.
- **Input fingerprinting:** each brief stores a SHA-256 hash of its submitted packet.
- **Privacy controls:** analysis is explicit, limited to text pasted in the AI Brief tab, and sent with `store: false`.

No API key? Use the clearly labeled local demo fixture. It exercises the complete review and audit workflow without a network request.

## Existing case-management features

- Cases with owners, deadlines, waiting states, tags, and workpaper locations
- Evidence checklist with Needed → Requested → Received → N/A states
- Append-only activity timeline
- Coverage, ownership-transfer, and help requests
- Recurring compliance obligations
- Today view for overdue and upcoming work
- Examiner package export to XLSX
- Calendar export to ICS
- Validated XLSX import
- Local SQLite workspace, daily backups, and readable Excel snapshots

## Quick start

Requires Go 1.24 or newer.

```sh
go build -o casebook .
./casebook
```

Casebook opens at `http://localhost:8484`. To allow teammates on a trusted local network to connect, start it with `./casebook -lan`.

### Enable live AI Briefs

```sh
export OPENAI_API_KEY="your-api-key"
./casebook
```

Optional: set `CASEBOOK_AI_MODEL` to override the default `gpt-5.6-sol` model.

Never commit a real API key. Casebook reads it from the process environment and does not store it in the workspace database.

## Run the synthetic demo

The in-app **Load synthetic demo** button uses a fictional report about Jordan Lee and Morgan Vale. The same input packet is available in [`demo/`](demo/) for repeatable testing. No real firm, employee, customer, or account data is included.

## Data and trust model

Everything except an explicitly submitted AI Brief lives in the workspace folder:

```text
My Compliance Casebook/
├─ workspace.cbk        # live SQLite data
├─ Records/             # auto-refreshed Excel snapshots
├─ Backups/             # daily backups; 14 copies retained
└─ Imports/             # validated import files
```

AI Briefs are opt-in. Casebook sends only the text pasted into that tab plus the case number, title, and current status. It does not automatically analyze other case fields, files, workpaper links, the database, or the workspace folder. Requests use the OpenAI Responses API with storage disabled.

Casebook is designed for a small team on a trusted network. Its member picker and optional PIN are not enterprise authentication. It does not provide SSO, RBAC, database encryption, or TLS. Do not use it with confidential or regulated data unless your organization has reviewed and approved the deployment and AI data flow.

## Development

```sh
go test ./...
go vet ./...
node --check web/app.js
go build -trimpath -o casebook .
```

CI runs the same checks on pushes and pull requests.

## Build Week provenance

This repository existed as a local case-management application before the competition. The Build Week submission is the evidence-grounded AI investigation workflow added after July 13, 2026. The pre-extension snapshot is tagged `pre-build-week-baseline`; the dated change record and old/new scope comparison are in [`docs/BUILD_WEEK.md`](docs/BUILD_WEEK.md).

## How I collaborated with Codex

Codex accelerated the work by mapping the existing Go/SQLite application, checking current OpenAI API guidance, implementing the Responses API and persistence layers, expanding automated tests, exercising the live model with synthetic data, performing browser-based UI QA, and preparing the release and submission materials.

I made the key product and risk decisions: the AI receives only text explicitly pasted for analysis; factual proposals need traceable quotes; Casebook verifies those quotes outside the model; the model cannot change a case without an item-level human decision; and every proposal and decision enters the normal audit trail. GPT-5.6-sol is the investigation analyst inside that boundary, while Codex was the engineering collaborator used to build and validate it.

Submission materials:

- [`docs/DEVPOST_SUBMISSION.md`](docs/DEVPOST_SUBMISSION.md) — ready-to-paste project description
- [`docs/DEMO_SCRIPT.md`](docs/DEMO_SCRIPT.md) — sub-three-minute video script
- [`docs/SECURITY.md`](docs/SECURITY.md) — data flow and safeguards
- [`docs/QA_CHECKLIST.md`](docs/QA_CHECKLIST.md) — manual release checks

The required Codex `/feedback` session was uploaded. Its Session ID is provided only through the Devpost submission form.

## Limitations

Casebook is not legal advice, a regulatory conclusion engine, or an enterprise GRC platform. AI output can be incomplete or wrong. Investigators must verify sources, apply their own judgment, and follow their organization's policies.

## License

[MIT](LICENSE)
