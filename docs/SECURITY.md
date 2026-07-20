# Security and AI data flow

Casebook is an early-stage local-first application for small teams on a trusted network. It is not an enterprise GRC or records platform.

## Local data

Case records, events, checklists, AI brief inputs and outputs, and review decisions are stored in the workspace's SQLite database. Casebook also creates local backups and spreadsheet snapshots. Anyone with filesystem access to the workspace can read or alter that data.

## Optional OpenAI request

Casebook makes an outbound request only when an authorized user explicitly generates a live AI Brief. That request contains:

- case number
- case title
- current case status
- report or intake narrative pasted into the AI Brief tab
- policy text pasted into the AI Brief tab
- evidence notes pasted into the AI Brief tab

It does not automatically read or transmit attachments, workpaper paths, the workspace database, other cases, member PINs, exports, backups, or files in the workspace folder.

The request uses the OpenAI Responses API with `store: false`. The API key is read from `OPENAI_API_KEY` at process startup/use and is not written to the Casebook database.

## AI controls

- Live analysis requires the case owner or active coverer.
- The interface displays the cloud-processing boundary before submission.
- Inputs have per-field and total character limits.
- Output must conform to a strict JSON schema.
- Factual items are expected to include source quotes.
- Casebook independently checks quotes against the submitted packet.
- Model output remains a proposal until a human accepts or rejects it.
- Review decisions are immutable through the application.
- Generation and review events are appended to the case history.

## Known limitations

- HTTP traffic between a browser and Casebook is not encrypted. Use localhost or a trusted network/VPN.
- Member selection and optional PINs are not strong authentication.
- The SQLite database is not encrypted at rest.
- AI inputs are retained locally in the database for auditability.
- Exact quote matching proves only that a quote occurs in the submitted text; it does not prove that the source is authentic or the interpretation is correct.
- Model output can be incomplete or wrong and requires qualified human review.

Do not use real confidential, personal, customer, or regulated data unless the deployment and OpenAI data flow have been approved by the relevant organization.

## Reporting a vulnerability

Do not open a public issue containing sensitive details. Use the repository owner's private security-reporting channel once the GitHub repository is published.
