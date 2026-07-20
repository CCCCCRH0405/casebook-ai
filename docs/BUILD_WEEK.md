# OpenAI Build Week change record

## Submission scope

Casebook existed before OpenAI Build Week as a local compliance case-management application. The competition submission is the evidence-grounded AI investigation workflow developed after the event's July 13, 2026 start date.

The pre-extension source is preserved by:

- baseline commit: `6edacd9 chore: record pre-Build Week Casebook baseline`
- annotated tag: `pre-build-week-baseline`
- Build Week branch: `codex/build-week`

This makes the new submission work reviewable as a normal Git diff.

## Before and after

| Capability | Pre-existing application | Build Week extension |
| --- | --- | --- |
| Case records | Owners, status, dates, descriptions, tags | Case metadata supplies limited context to an explicit AI run |
| Evidence | Manual checklist | AI proposes evidence gaps; accepted gaps become checklist items |
| Activity | Append-only human actions | Adds `AI_PROPOSED`, `AI_ACCEPTED`, and `AI_REJECTED` events |
| Analysis | No model-assisted investigation workflow | GPT-5.6-sol allegations, timeline, policy mapping, gaps, conflicts, risks, actions, and questions |
| Grounding | Not applicable | Required source quotes plus server-side exact-match verification |
| Human control | Manual case edits | Item-level accept/reject decisions; no silent AI mutation |
| Privacy | Local workspace | Explicit opt-in; only pasted text is sent; Responses API uses `store: false` |
| Demo | Sample case data | Deterministic, clearly labeled synthetic AI fixture without an API key |

## Technical implementation

- OpenAI Responses API with `gpt-5.6-sol`
- Medium reasoning effort and strict JSON Schema structured output
- Per-user hashed safety identifier
- Input size limits and SHA-256 packet fingerprint
- Local exact-quote grounding after generation
- Persistent AI brief and immutable item-decision tables in SQLite
- Permission checks: only the case owner or active coverer can generate or review a brief
- Idempotent decision handling and duplicate checklist protection
- End-to-end tests with a mock Responses API

## Verification evidence

On July 17, 2026, the workflow was tested against the live OpenAI Responses API using only the repository's fictional Jordan Lee/Morgan Vale packet:

- requested model: `gpt-5.6-sol`
- live API response completed successfully; its response identifier is intentionally omitted from the public repository
- source citations returned: 32
- citations verified as exact source matches: 32

The live response was not committed. The repository contains only synthetic source material and the deterministic local fixture.

Release checks:

```sh
go test ./...
go vet ./...
node --check web/app.js
go build -trimpath -o casebook .
```

## How Codex was used

Codex helped inspect the pre-existing application, define the narrow Build Week scope, implement the Responses API and audit workflow, add tests, exercise the live API with synthetic data, perform browser-based UI QA, and prepare repository and submission materials. Product judgment, source code, and intellectual-property ownership remain with the entrant.

The required `/feedback` upload completed successfully. Its Session ID is intentionally omitted from the public repository and provided only through Devpost.
