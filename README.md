# Casebook — compliance case management

A local case tracker for small compliance teams. Runs from a folder — no server, no cloud, no IT ticket.

## The story

I work in compliance. Outside the daily routine, the real pain is keeping track of everything else — inquiries, filings, reviews, remediation items, recurring obligations, the perpetual "where are we on this?" question from management. I built this for myself because spreadsheets kept falling apart under that load: cells overwritten with no history, ownership unclear, recurring items missed, no easy way to show an examiner what happened and when.

I probably can't even use it at my own firm — IT only gets stricter every year — so I'm open-sourcing it for anyone whose team still runs on Excel.

## Who it's for

Small compliance teams (roughly 3–10 people) who want to track working cases, recurring obligations, and evidence collection without standing up infrastructure. One person runs the binary; teammates use a browser on the same network.

## Who it's not for

- Enterprise or IT-managed environments (there is no SSO, no LDAP, no audit-grade access control)
- Teams that need a GRC platform, risk register, or policy management system
- Anyone looking for regulatory guidance or compliance advice — this tool contains none
- Cloud-hosted or multi-office deployments

If you need real authentication, role-based access control, or a server that survives the host laptop closing, this is not that.

## Quick start

### Download the binary

1. Download the binary for your OS from the Releases page.
2. Put it anywhere — your desktop, a shared drive, wherever the team can reach it.
3. Double-click to run. A first-run wizard walks you through picking a folder, naming your team, and adding teammates.
4. The app prints a LAN URL (`http://<your-ip>:8484`). Teammates open that in any browser — nothing to install on their end.

### Build from source

Requires Go 1.22+.

```sh
git clone https://github.com/yourusername/compliance-case-management
cd compliance-case-management
go build -o casebook ./cmd/casebook
./casebook
```

No CGO required. The SQLite driver is pure Go.

## The workspace folder

Everything lives in one folder you pick at first run:

```
My Compliance Casebook/
├─ workspace.cbk        ← all live data (SQLite; backup = copy this file)
├─ Records/             ← auto-refreshed Excel snapshots (read-only)
│   ├─ Cases_2026-06.xlsx
│   └─ ActivityLog_2026-06.xlsx
├─ Backups/             ← daily auto-zips, 14 copies kept
└─ Imports/             ← drop import files here
```

**Backup = copy the folder.** That's it. No database server, no backup agent, no export ritual.

## Features

**First-run wizard.** Pick a folder, name your team, add teammates. A sample-data option lets you load a demo workspace and see the app in action before entering real data.

**Quick-add.** Press `/` anywhere, type a title, hit Enter. The case is recorded in under ten seconds; fill in details later.

**Cases.** Each case has: type (Inquiry / Filing / Testing / Review / Remediation / Request / Task — customizable in Settings), owner, next-action due date, hard deadline, source, workpaper location (a path or link — files are never uploaded), tags, and description.

**Status state machine.** Open → In Progress → Waiting → Completed → Archived, with Cancelled reachable from any state. Waiting requires picking who you're waiting on (Business Unit, Legal, IT, Regulator, Vendor, Counterparty, or Other) — this becomes a structured field, not a buried note, so you can report on it. Completed cases auto-archive after a 7-day cool-off. Cancelled requires a reason. Any case can be reopened. Every transition is appended to an immutable audit timeline.

**Evidence checklist.** Each case has an evidence tab. Items move through Needed → Requested → Received → N/A. Every change is audited.

**Audit timeline.** Every case carries a full append-only event history: field changes, status transitions, notes, assignments, requests, imports. Nothing is ever updated or deleted. If something was recorded wrong, you append a correction.

**Collaboration.** Three request types live in the Inbox:
- *Coverage request* — "cover for me while I'm out." The accepted coverer gets edit rights on the case for the duration.
- *Ownership transfer* — current owner must approve before the case moves.
- *Help request* — a lightweight nudge that creates an Inbox item without transferring ownership.

**Recurring obligations.** Yearly, Quarterly, Monthly, or Weekly items with configurable lead time. Each obligation carries a checklist template that is copied into every auto-created case. The obligation detail page lists all historical cases for that obligation. You can also trigger "Create now" manually. The engine runs at startup and nightly at 00:05.

**Today page.** Overdue, due today, due in the next 7 days, items you're waiting on, and cases you're covering — one page, no hunting.

**Cases table.** Full list with filters (type, status, owner, tags, date range) and text search. Filters can be saved.

**Examiner package.** Select a date range, case types, and members; export a single xlsx with a summary sheet, all matching cases, full event histories, obligations, and members. Filename includes a timestamp.

**Calendar export.** All due dates and deadlines export as an .ics file that Outlook, Google Calendar, or Apple Calendar can subscribe to directly.

**Import.** Download a template xlsx, fill it in, drop it in `Imports/` or upload it from the Import page. Every row is validated before you can commit; errors are shown row-by-row with the specific problem. Nothing is silently skipped.

## The trust model

Casebook is designed for a small team sharing an office network. The trust level is comparable to a shared spreadsheet on a shared drive.

Identity works like this: when you open the app, you pick your name from the team list. If you set a PIN (4–8 digits), you enter it at that screen. That is the full extent of access control.

The PIN is an honest speed bump for shared-office scenarios — it slows down accidental or casual access in a room where someone might sit at an unlocked laptop. It does not protect against anyone determined to access the data, does not encrypt the database, and is not a substitute for real authentication. An admin can clear a forgotten PIN but cannot set one for another member (PINs are optional, and the member sets their own).

What Casebook does not provide: network encryption (use a VPN or trusted LAN), user authentication, role-based access control, or any protection against someone with filesystem access to the host machine reading `workspace.cbk` directly (it is a standard SQLite file).

If your threat model requires any of those, this tool is not appropriate for your environment.

## FAQ

**Is my data uploaded anywhere?**
No. The binary makes no outbound network connections. All data stays in the folder you picked.

**Why does it need a port?**
It runs a small HTTP server on port 8484 so teammates can connect from their browsers. Nothing is exposed outside your LAN unless you configure your router or firewall to do so (don't).

**What happens when the host laptop closes?**
Teammates see a clear "Host is offline" page with instructions on who to contact. Nothing corrupts; the data is safe. Reopen the laptop and the app is available again.

**Why SQLite and not Excel as the live store?**
Excel files are not safe for concurrent writes and have no audit trail. Casebook uses SQLite (with WAL mode) as the live database and writes Excel snapshots to `Records/` automatically — so anyone who wants to open a spreadsheet and read the current state can do so at any time, without that spreadsheet being the thing you write to.

**Can FINRA, the SEC, or internal auditors get records from this?**
Yes, that's the point. Use the Examiner Package export: select the date range and scope, and you get a single xlsx with full case history and event timelines. Nothing requires live access to the running application.

## License

MIT. See LICENSE.

## Status

Early-stage. Built with heavy AI assistance by one compliance professional. The data model and state machine are intentional; the UI and edge cases still need work. Feedback from anyone who has actually run a compliance function is especially welcome — open an issue or start a discussion.
