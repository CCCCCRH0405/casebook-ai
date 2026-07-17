"use strict";

let BOOT = null;
let ME = parseInt(localStorage.getItem("cb_member") || "0", 10) || 0;
let casesFilters = { q: "", status: "", owner: "", type: "", view: "active" };
let CASE_RTAB = "activity";

// ---------- utilities ----------

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function clearIdentity() {
  ME = 0;
  localStorage.removeItem("cb_member");
  localStorage.removeItem("cb_pin");
}

async function api(path, opts = {}) {
  const headers = { "Content-Type": "application/json" };
  if (ME) {
    headers["X-Member"] = String(ME);
    const pin = localStorage.getItem("cb_pin");
    if (pin) headers["X-Pin"] = pin;
  }
  const res = await fetch(path, {
    method: opts.method || "GET",
    headers,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  let data = {};
  try { data = await res.json(); } catch (e) { /* empty body */ }
  if (!res.ok) throw new Error(data.error || ("request failed (" + res.status + ")"));
  return data;
}

let toastTimer = null;
function toast(msg, isErr) {
  const t = document.getElementById("toast");
  t.textContent = msg;
  t.className = "toast show" + (isErr ? " err" : "");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (t.className = "toast"), 3600);
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
function fmtDate(d) {
  if (!d) return "";
  const [y, m, day] = d.split("-").map(Number);
  if (!y || !m || !day) return d;
  const now = new Date();
  return MONTHS[m - 1] + " " + day + (y !== now.getFullYear() ? ", " + y : "");
}
function fmtWhen(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d)) return iso;
  return MONTHS[d.getMonth()] + " " + d.getDate() + " " +
    String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
}
function todayStr() {
  const d = new Date();
  return d.getFullYear() + "-" + String(d.getMonth() + 1).padStart(2, "0") + "-" + String(d.getDate()).padStart(2, "0");
}
function daysSince(d) {
  if (!d) return 0;
  return Math.max(0, Math.round((new Date(todayStr()) - new Date(d)) / 86400000));
}
function memberName(id) {
  const m = (BOOT.members || []).find(x => x.id === id);
  return m ? m.name : "";
}
function typeName(id) {
  const t = (BOOT.types || []).find(x => x.id === id);
  return t ? t.name : "";
}
function statusDot(s) {
  const cls = s.toLowerCase().replace(/\s+/g, "");
  return '<span class="dot ' + cls + '"></span>';
}
function isClosedStatus(s) {
  return s === "Completed" || s === "Archived" || s === "Cancelled";
}

// ---------- shell & routing ----------

function app() { return document.getElementById("app"); }

async function init() {
  BOOT = await api("/api/bootstrap");
  if (!BOOT.setup_done) { renderSetup(); return; }
  const me = (BOOT.members || []).find(m => m.id === ME && m.active);
  if (!me) { clearIdentity(); renderWho(); return; }
  if (me.has_pin) {
    try {
      await api("/api/login", { method: "POST", body: { member_id: ME, pin: localStorage.getItem("cb_pin") || "" } });
    } catch (_) { clearIdentity(); renderWho(); return; }
  }
  renderShell();
  route();
}

window.addEventListener("hashchange", () => { if (BOOT && BOOT.setup_done && ME) route(); });

function route() {
  const h = location.hash || "#/today";
  document.querySelectorAll(".nav-links a").forEach(a => {
    a.classList.toggle("on", h.startsWith(a.getAttribute("href")) ||
      (a.getAttribute("href") === "#/cases" && h.startsWith("#/case/")));
  });
  updateInboxBadge();
  if (h.startsWith("#/case/")) return viewCase(decodeURIComponent(h.slice(7)));
  if (h.startsWith("#/cases")) return viewCases();
  if (h.startsWith("#/recurring")) return viewRecurring();
  if (h.startsWith("#/calendar")) return viewCalendar();
  if (h.startsWith("#/inbox")) return viewInbox();
  if (h.startsWith("#/reports")) return viewReports();
  if (h.startsWith("#/import")) return viewImport();
  if (h.startsWith("#/settings")) return viewSettings();
  return viewToday();
}

async function updateInboxBadge() {
  const b = document.getElementById("inbox-badge");
  if (!b) return;
  try {
    const d = await api("/api/inbox/count");
    b.textContent = d.count;
    b.style.display = d.count > 0 ? "" : "none";
  } catch (_) { /* badge is cosmetic */ }
}

function renderShell() {
  const me = BOOT.members.find(m => m.id === ME);
  app().innerHTML =
    '<div class="nav">' +
    '<span class="brand">Casebook</span>' +
    '<div class="nav-links">' +
    '<a href="#/today">Today</a>' +
    '<a href="#/cases">Cases</a>' +
    '<a href="#/recurring">Recurring</a>' +
    '<a href="#/calendar">Calendar</a>' +
    '<a href="#/inbox">Inbox <span class="badge-mini" id="inbox-badge" style="display:none"></span></a>' +
    '<a href="#/reports">Reports</a>' +
    '<a href="#/import">Import</a>' +
    '<a href="#/settings">Settings</a>' +
    "</div>" +
    '<div class="nav-right">' +
    '<span class="team-label">' + esc(BOOT.team_name) + "</span>" +
    '<button class="avatar" id="whoami" title="Signed in as ' + esc(me.name) + ' — click to switch">' + esc(me.initials) + "</button>" +
    "</div></div>" +
    '<div class="quickadd">' +
    '<input id="quickadd" placeholder="Add a case — type a title, press Enter. It lands on your list; details can wait.">' +
    '<span class="kbd">/</span>' +
    '<button class="btn" id="newcase-btn">+ New case</button>' +
    "</div>" +
    '<div id="view"></div>' +
    '<div id="modal-root"></div>';
  document.getElementById("whoami").onclick = () => { clearIdentity(); renderWho(); };
  const qa = document.getElementById("quickadd");
  qa.addEventListener("keydown", async e => {
    if (e.key !== "Enter") return;
    const title = qa.value.trim();
    if (!title) return;
    qa.disabled = true;
    try {
      const r = await api("/api/cases", { method: "POST", body: { title, owner_id: ME } });
      qa.value = "";
      toast(r.case.case_no + " created");
      route();
    } catch (err) { toast(err.message, true); }
    qa.disabled = false;
    qa.focus();
  });
  document.getElementById("newcase-btn").onclick = () => openNewCase(qa.value.trim());
  document.addEventListener("keydown", e => {
    if (e.key === "Escape") closeModal();
    if (e.key === "/" && !/INPUT|TEXTAREA|SELECT/.test(document.activeElement.tagName)) {
      e.preventDefault();
      qa.focus();
    }
  });
}

// ---------- modal + new case ----------

function closeModal() {
  const r = document.getElementById("modal-root");
  if (r) r.innerHTML = "";
}

function openModal(html) {
  const r = document.getElementById("modal-root");
  r.innerHTML = '<div class="modal-overlay" id="modal-ov"><div class="modal" role="dialog">' + html + "</div></div>";
  document.getElementById("modal-ov").onclick = e => { if (e.target.id === "modal-ov") closeModal(); };
}

function openNewCase(prefillTitle) {
  const typeOpts = '<option value="0">—</option>' +
    BOOT.types.filter(t => t.active).map(t => '<option value="' + t.id + '">' + esc(t.name) + "</option>").join("");
  const ownerOpts = '<option value="' + ME + '">' + esc(memberName(ME)) + " (me)</option>" +
    '<option value="0">Unassigned</option>' +
    BOOT.members.filter(m => m.active && m.id !== ME).map(m => '<option value="' + m.id + '">' + esc(m.name) + "</option>").join("");
  openModal(
    '<div class="nc">' +
    '<input id="nc-title" class="nc-hero" placeholder="New case — what needs tracking?" value="' + esc(prefillTitle || "") + '">' +
    '<div class="nc-row">' +
    '<select id="nc-type" class="nc-pick"><option value="0">Type</option>' + typeOpts + "</select>" +
    '<select id="nc-owner" class="nc-pick">' + ownerOpts + "</select>" +
    '<label class="nc-date">Due <input type="date" id="nc-due"></label>' +
    "</div>" +
    '<button class="nc-more-toggle" id="nc-more-toggle"><span id="nc-more-caret">＋</span> More details</button>' +
    '<div id="nc-more" class="nc-more" hidden>' +
    '<label class="nc-date" style="margin-bottom:10px">Hard deadline <input type="date" id="nc-deadline"></label>' +
    '<input id="nc-source" class="nc-line" placeholder="Source — where it came from">' +
    '<input id="nc-tags" class="nc-line" placeholder="Tags (comma separated)">' +
    '<textarea id="nc-desc" class="nc-line" rows="2" placeholder="Notes"></textarea>' +
    "</div>" +
    '<div class="nc-foot">' +
    '<button class="btn primary" id="nc-create">Create case</button>' +
    '<button class="btn" id="nc-cancel">Cancel</button>' +
    '<label class="nc-again"><input type="checkbox" id="nc-another"> Add another</label>' +
    "</div></div>");
  const typeSel = document.getElementById("nc-type");
  const titleEl = document.getElementById("nc-title");
  titleEl.focus();
  titleEl.addEventListener("keydown", e => { if (e.key === "Enter") document.getElementById("nc-create").click(); });
  document.getElementById("nc-more-toggle").onclick = () => {
    const m = document.getElementById("nc-more");
    m.hidden = !m.hidden;
    document.getElementById("nc-more-caret").textContent = m.hidden ? "＋" : "－";
  };
  document.getElementById("nc-cancel").onclick = closeModal;
  document.getElementById("nc-create").onclick = async () => {
    const title = titleEl.value.trim();
    if (!title) { toast("Title is required", true); titleEl.focus(); return; }
    const body = {
      title,
      type_id: parseInt(document.getElementById("nc-type").value, 10) || 0,
      owner_id: parseInt(document.getElementById("nc-owner").value, 10) || 0,
      due_date: document.getElementById("nc-due").value,
      deadline: document.getElementById("nc-deadline").value,
      source: document.getElementById("nc-source").value.trim(),
      tags: document.getElementById("nc-tags").value.trim(),
      description: document.getElementById("nc-desc").value.trim(),
    };
    const btn = document.getElementById("nc-create");
    btn.disabled = true;
    try {
      const r = await api("/api/cases", { method: "POST", body });
      const again = document.getElementById("nc-another").checked;
      toast(r.case.case_no + " created");
      const qa = document.getElementById("quickadd");
      if (qa) qa.value = "";
      if (again) { openNewCase(""); }
      else { closeModal(); if (location.hash.startsWith("#/case/")) location.hash = "#/cases"; else route(); }
    } catch (err) { toast(err.message, true); btn.disabled = false; }
  };
}

function view() { return document.getElementById("view"); }

// ---------- setup wizard ----------

function renderSetup() {
  app().innerHTML =
    '<div class="wizard">' +
    '<span class="brand">Casebook</span>' +
    "<h1>Set up your workspace</h1>" +
    "<p>Everything stays in the folder this program runs from. No account, no cloud.</p>" +
    '<div class="f"><label>Team name</label><input id="su-team" placeholder="e.g. Compliance"></div>' +
    '<div class="f"><label>Your name</label><input id="su-you" placeholder="e.g. Dana Mori"></div>' +
    '<div class="f"><label>Teammates — one per line (you can add more later)</label>' +
    '<textarea id="su-members" rows="3" placeholder="optional"></textarea></div>' +
    '<button class="btn primary" id="su-go">Create workspace</button>' +
    "</div>";
  document.getElementById("su-go").onclick = async () => {
    const team = document.getElementById("su-team").value.trim();
    const you = document.getElementById("su-you").value.trim();
    const members = document.getElementById("su-members").value.split("\n").map(s => s.trim()).filter(Boolean);
    if (!team || !you) { toast("team name and your name are required", true); return; }
    try {
      const r = await api("/api/setup", { method: "POST", body: { team, you, members } });
      ME = r.you;
      localStorage.setItem("cb_member", String(ME));
      await init();
      toast("Workspace ready");
    } catch (err) { toast(err.message, true); }
  };
}

function renderWho() {
  const names = BOOT.members.filter(m => m.active).map(m =>
    '<button data-id="' + m.id + '" data-pin="' + (m.has_pin ? 1 : 0) + '">' + esc(m.name) +
    (m.has_pin ? ' <span class="t-faint">· PIN</span>' : "") + "</button>").join("");
  app().innerHTML =
    '<div class="who"><h1>Who are you?</h1><p>' + esc(BOOT.team_name) + " · Casebook</p>" +
    '<div class="names">' + names + "</div>" +
    '<div id="who-pin"></div></div>';
  const signIn = async (id, pin) => {
    try {
      await api("/api/login", { method: "POST", body: { member_id: id, pin: pin || "" } });
      ME = id;
      localStorage.setItem("cb_member", String(ME));
      if (pin) localStorage.setItem("cb_pin", pin); else localStorage.removeItem("cb_pin");
      renderShell();
      route();
    } catch (err) { toast(err.message, true); }
  };
  document.querySelectorAll(".who .names button").forEach(b => {
    b.onclick = () => {
      const id = parseInt(b.dataset.id, 10);
      if (b.dataset.pin !== "1") return signIn(id, "");
      document.getElementById("who-pin").innerHTML =
        '<div class="inlineform" style="justify-content:center;padding-top:18px">' +
        '<span class="t-muted">PIN for ' + esc(b.textContent.replace("· PIN", "").trim()) + "</span>" +
        '<input id="wp-pin" type="password" inputmode="numeric" maxlength="8" style="width:110px" autofocus>' +
        '<button class="btn primary" id="wp-go">Sign in</button></div>';
      const input = document.getElementById("wp-pin");
      input.focus();
      const go = () => signIn(id, input.value.trim());
      document.getElementById("wp-go").onclick = go;
      input.addEventListener("keydown", e => { if (e.key === "Enter") go(); });
    };
  });
}

// ---------- shared table ----------

function caseTable(cases, opts = {}) {
  if (!cases.length) return '<div class="empty">' + (opts.empty || "Nothing here.") + "</div>";
  const td = todayStr();
  const rows = cases.map(c => {
    const over = c.due_date && c.due_date < td && !["Completed", "Archived", "Cancelled"].includes(c.status);
    const waiting = c.waiting_on
      ? esc(c.waiting_on) + ' <span class="t-faint">· ' + daysSince(c.waiting_since) + "d</span>"
      : '<span class="t-faint">—</span>';
    return '<tr class="row" data-no="' + esc(c.case_no) + '">' +
      '<td class="c-no">' + esc(c.case_no) + "</td>" +
      "<td>" + esc(c.title) + "</td>" +
      '<td class="t-muted">' + (esc(c.type_name) || '<span class="t-faint">—</span>') + "</td>" +
      (opts.showOwner === false ? "" :
        '<td class="t-muted">' + (esc(c.owner_name) || '<span class="t-faint">unassigned</span>') +
        (c.coverer_name ? ' <span class="t-faint">⤷ ' + esc(c.coverer_name) + "</span>" : "") + "</td>") +
      "<td>" + waiting + "</td>" +
      '<td class="c-due' + (over ? " over" : "") + '">' + (fmtDate(c.due_date) || '<span class="t-faint">—</span>') + "</td>" +
      "<td>" + statusDot(c.status) + '<span class="t-muted">' + esc(c.status) + "</span></td>" +
      "</tr>";
  }).join("");
  return '<table class="list"><thead><tr>' +
    '<th class="c-no">Case</th><th>Title</th><th>Type</th>' +
    (opts.showOwner === false ? "" : "<th>Owner</th>") +
    "<th>Waiting on</th><th>Due</th><th>Status</th>" +
    "</tr></thead><tbody>" + rows + "</tbody></table>";
}

function bindRows(root) {
  root.querySelectorAll("tr.row").forEach(tr => {
    tr.onclick = () => { location.hash = "#/case/" + encodeURIComponent(tr.dataset.no); };
  });
}

// ---------- today ----------

async function viewToday() {
  view().innerHTML = '<div class="page"><div class="empty">Loading…</div></div>';
  let d;
  try { d = await api("/api/today"); } catch (err) {
    view().innerHTML = '<div class="page"><div class="empty">' + esc(err.message) + "</div></div>";
    return;
  }
  const open = d.overdue.length + d.due_today.length + d.due_week.length + d.rest.length + d.waiting.length;
  const stat = (n, label, cls) =>
    '<span class="stat ' + (cls || "") + '"><b>' + n + "</b><span>" + label + "</span></span>";
  const sec = (label, cases, empty) =>
    '<div class="sec"><div class="sec-label">' + label +
    ' <span class="count">' + cases.length + "</span></div>" +
    caseTable(cases, { showOwner: false, empty }) + "</div>";
  view().innerHTML =
    '<div class="page">' +
    '<div class="statline">' +
    stat(d.overdue.length, "overdue", d.overdue.length ? "bad" : "") +
    stat(d.due_today.length, "due today") +
    stat(d.waiting.length, "waiting on others", d.waiting.length ? "warn" : "") +
    stat(open, "open cases") +
    (d.unassigned_count ? '<span class="stat"><b>' + d.unassigned_count +
      '</b><span><a href="#/cases?owner=unassigned">unassigned</a></span></span>' : "") +
    "</div>" +
    (d.overdue.length ? sec("Overdue", d.overdue) : "") +
    sec("Due today", d.due_today, "Nothing due today.") +
    sec("Next 7 days", d.due_week, "Nothing due this week.") +
    sec("Waiting on someone", d.waiting, "Nothing is blocked. Enjoy it while it lasts.") +
    (d.covering.length ? '<div class="sec"><div class="sec-label">Covering for a teammate <span class="count">' +
      d.covering.length + "</span></div>" + caseTable(d.covering, {}) + "</div>" : "") +
    sec("Everything else", d.rest, "No other open cases.") +
    "</div>";
  bindRows(view());
}

// ---------- cases ----------

async function viewCases() {
  const qs = (location.hash.split("?")[1] || "");
  const params = new URLSearchParams(qs);
  if (params.get("owner")) casesFilters.owner = params.get("owner");
  const f = casesFilters;
  const memberOpts = ['<option value="">Everyone</option>', '<option value="unassigned">Unassigned</option>']
    .concat(BOOT.members.filter(m => m.active).map(m =>
      '<option value="' + m.id + '">' + esc(m.name) + "</option>")).join("");
  const typeOpts = ['<option value="">All types</option>']
    .concat(BOOT.types.filter(t => t.active).map(t =>
      '<option value="' + t.id + '">' + esc(t.name) + "</option>")).join("");
  const statusOpts = ['<option value="">Any status</option>']
    .concat(["Open", "In Progress", "Waiting", "Completed"].map(s =>
      "<option>" + s + "</option>")).join("");
  view().innerHTML =
    '<div class="page page-wide">' +
    '<div class="filters">' +
    '<input type="text" id="fq" placeholder="Search title, number, tags…" value="' + esc(f.q) + '">' +
    '<select id="fstatus">' + statusOpts + "</select>" +
    '<select id="fowner">' + memberOpts + "</select>" +
    '<select id="ftype">' + typeOpts + "</select>" +
    '<select id="fview"><option value="active">Active</option><option value="archived">Archived</option><option value="all">Everything</option></select>' +
    "</div>" +
    '<div id="cases-table"><div class="empty">Loading…</div></div>' +
    "</div>";
  document.getElementById("fstatus").value = f.status;
  document.getElementById("fowner").value = f.owner;
  document.getElementById("ftype").value = f.type;
  document.getElementById("fview").value = f.view;
  const refresh = async () => {
    f.q = document.getElementById("fq").value.trim();
    f.status = document.getElementById("fstatus").value;
    f.owner = document.getElementById("fowner").value;
    f.type = document.getElementById("ftype").value;
    f.view = document.getElementById("fview").value;
    const p = new URLSearchParams();
    if (f.q) p.set("q", f.q);
    if (f.status) p.set("status", f.status);
    if (f.owner) p.set("owner", f.owner);
    if (f.type) p.set("type", f.type);
    if (f.view !== "active") p.set("view", f.view);
    try {
      const d = await api("/api/cases?" + p.toString());
      const el = document.getElementById("cases-table");
      el.innerHTML = caseTable(d.cases, { empty: "No cases match." });
      bindRows(el);
    } catch (err) { toast(err.message, true); }
  };
  let deb = null;
  document.getElementById("fq").addEventListener("input", () => {
    clearTimeout(deb);
    deb = setTimeout(refresh, 250);
  });
  ["fstatus", "fowner", "ftype", "fview"].forEach(id =>
    document.getElementById(id).addEventListener("change", refresh));
  await refresh();
}

// ---------- case detail ----------

const FIELD_LABELS = {
  title: "Title", type_id: "Type", due_date: "Next action due", deadline: "Hard deadline",
  source: "Source", location: "Workpaper location", tags: "Tags", description: "Description",
  owner_id: "Owner", waiting_on: "Waiting on", waiting_detail: "Waiting detail",
};

function eventText(e) {
  let p = {};
  try { p = JSON.parse(e.payload || "{}"); } catch (_) {}
  switch (e.kind) {
    case "CREATED": return "created this case";
    case "NOTE": return esc(p.text || "");
    case "ASSIGNED":
      if (p.claim) return "claimed this case";
      if (p.to) return "assigned to " + esc(p.to);
      return changeList(p);
    case "STATUS": {
      let s = '<span class="delta">' + esc(p.from) + " → </span>" + esc(p.to);
      if (p.waiting_on) s += '<span class="delta"> · waiting on ' + esc(p.waiting_on) + (p.detail ? " — " + esc(p.detail) : "") + "</span>";
      if (p.reason) s += '<span class="delta"> · ' + esc(p.reason) + "</span>";
      return s;
    }
    case "REOPENED": return "reopened" + (p.from ? ' <span class="delta">(was ' + esc(p.from) + ")</span>" : "");
    case "ARCHIVED": return p.auto ? "archived automatically after the cool-off period" : "archived";
    case "FIELD_CHANGE": return changeList(p);
    case "CHECKLIST":
      if (p.action === "added") return "added checklist item: " + esc(p.label);
      if (p.action === "removed") return "removed checklist item: " + esc(p.label);
      return esc(p.label) + ' <span class="delta">→ ' + esc(p.state) + "</span>";
    case "REQUEST":
      if (p.withdrawn) return "withdrew a " + esc((p.kind || "").toLowerCase()) + " request";
      return "asked " + esc(p.to) + " — " + esc((p.kind || "").toLowerCase()) +
        (p.message ? ': <span class="delta">' + esc(p.message) + "</span>" : "");
    case "DECISION":
      return (p.decision === "ACCEPTED" ? "accepted" : "declined") + " the " +
        esc((p.kind || "").toLowerCase()) + " request from " + esc(p.from);
    case "COVERAGE":
      return p.action === "started"
        ? esc(p.coverer) + " started covering for " + esc(p.owner)
        : "coverage by " + esc(p.coverer) + " ended";
    case "SPAWNED":
      return "created " + (p.auto ? "automatically " : "") + "from recurring item: " +
        esc(p.obligation) + ' <span class="delta">(' + esc(p.occurrence) + ")</span>";
    case "RECURRING":
      return "made this recurring — " + esc(p.frequency).toLowerCase() + ", next due " + esc(p.next_due) +
        (p.checklist_items ? ', checklist template carried over (' + p.checklist_items + " items)" : "");
    default: return esc(e.kind.toLowerCase());
  }
}

function changeList(p) {
  if (!p.changes) return "";
  return p.changes.map(ch => {
    let from = ch.from, to = ch.to;
    if (ch.field === "type_id") { from = typeName(+from) || "—"; to = typeName(+to) || "—"; }
    if (ch.field === "owner_id") { from = memberName(+from) || "unassigned"; to = memberName(+to) || "unassigned"; }
    return esc(FIELD_LABELS[ch.field] || ch.field) + ': <span class="delta">' +
      (esc(from) || "—") + " → </span>" + (esc(to) || "—");
  }).join("<br>");
}

const STATUS_ACTIONS = {
  "In Progress": c => (["Completed", "Archived", "Cancelled"].includes(c.status) ? "Reopen" : "Start"),
  "Open": c => (c.status === "Cancelled" ? "Reopen" : "Back to open"),
  "Waiting": () => "Mark waiting…",
  "Completed": () => "Complete",
  "Cancelled": () => "Cancel…",
  "Archived": () => "Archive now",
};

async function viewCase(no) {
  view().innerHTML = '<div class="page"><div class="empty">Loading…</div></div>';
  let d;
  try { d = await api("/api/cases/" + encodeURIComponent(no)); } catch (err) {
    view().innerHTML = '<div class="page"><div class="empty">' + esc(err.message) + "</div></div>";
    return;
  }
  const c = d.case;
  const mine = c.owner_id === ME;
  const covering = c.coverer_id === ME && !mine;
  const canAct = mine || covering || c.owner_id === 0;
  const targets = (BOOT.transitions[c.status] || []);
  const actionBtns = !canAct ? "" : targets.map(t => {
    const label = (STATUS_ACTIONS[t] || (() => t))(c);
    const cls = t === "Completed" ? "btn primary" : t === "Cancelled" ? "btn danger" : "btn";
    return '<button class="' + cls + '" data-to="' + esc(t) + '">' + esc(label) + "</button>";
  }).join("");
  let peopleBtns = "";
  if (!isClosedStatus(c.status)) {
    if (mine) peopleBtns += '<button class="btn" data-req="COVERAGE">Ask someone to cover…</button>';
    if (!mine && c.owner_id !== 0 && !covering) peopleBtns += '<button class="btn" data-req="TRANSFER">Request transfer…</button>';
    if (c.owner_id !== 0) peopleBtns += '<button class="btn" data-req="HELP">Ask for help…</button>';
    if (canAct && !c.obligation_id) peopleBtns += '<button class="btn" data-mkrec="1">Make recurring…</button>';
  }
  const ownerCtl = c.owner_id === 0
    ? '<button class="btn quiet" id="claim">Claim this case</button>'
    : '<span class="t-muted">Owner</span> <b>' + esc(c.owner_name) + "</b>";
  const coverCtl = c.coverer_name
    ? '<span class="t-muted" style="margin-left:10px">covered by <b>' + esc(c.coverer_name) + "</b></span>" +
      ((mine || covering) ? ' <button class="btn quiet" id="end-cover">End coverage</button>' : "")
    : "";
  const reqBlock = (d.requests || []).map(rq => {
    let btns = "";
    if (rq.to_id === ME) {
      btns = '<button class="btn primary" data-rq-accept="' + rq.id + '">Accept</button> ' +
        '<button class="btn" data-rq-decline="' + rq.id + '">Decline</button>';
    } else if (rq.from_id === ME) {
      btns = '<button class="btn quiet" data-rq-withdraw="' + rq.id + '">Withdraw</button>';
    }
    return '<div class="req-item"><span class="req-kind">' + esc(rq.kind.toLowerCase()) + "</span>" +
      "<b>" + esc(rq.from_name) + '</b> <span class="t-faint">→</span> <b>' + esc(rq.to_name) + "</b>" +
      (rq.message ? ' <span class="t-muted">— ' + esc(rq.message) + "</span>" : "") +
      '<span class="req-actions">' + btns + "</span></div>";
  }).join("");
  const typeOpts = ['<option value="0">—</option>'].concat(
    BOOT.types.filter(t => t.active || t.id === c.type_id).map(t =>
      '<option value="' + t.id + '"' + (t.id === c.type_id ? " selected" : "") + ">" + esc(t.name) + "</option>")).join("");
  const ownerOpts = ['<option value="0"' + (c.owner_id === 0 ? " selected" : "") + ">Unassigned</option>"].concat(
    BOOT.members.filter(m => m.active || m.id === c.owner_id).map(m =>
      '<option value="' + m.id + '"' + (m.id === c.owner_id ? " selected" : "") + ">" + esc(m.name) + "</option>")).join("");
  const waitInfo = c.status === "Waiting" && c.waiting_on
    ? '<span class="t-muted">· waiting on <b>' + esc(c.waiting_on) + "</b> for " + daysSince(c.waiting_since) + "d" +
      (c.waiting_detail ? " — " + esc(c.waiting_detail) : "") + "</span>"
    : "";

  view().innerHTML =
    '<div class="page page-wide">' +
    '<div class="case-head">' +
    '<div class="no">' + esc(c.case_no) + (c.obligation_id ? ' <span class="t-faint">· ↻ recurring</span>' : "") + "</div>" +
    '<input class="title" id="f-title" value="' + esc(c.title) + '"' + (canAct ? "" : " readonly") + ">" +
    '<div class="statusline">' + statusDot(c.status) + "<b>" + esc(c.status) + "</b> " + waitInfo +
    '<span style="margin-left:14px">' + ownerCtl + "</span>" + coverCtl +
    '<span class="actions">' + actionBtns + peopleBtns + "</span>" +
    "</div>" +
    '<div id="inline-form"></div>' +
    (reqBlock ? '<div class="req-list">' + reqBlock + "</div>" : "") +
    "</div>" +
    '<div class="case-grid">' +
    '<div class="fields">' +
    field("Type", '<select id="f-type_id">' + typeOpts + "</select>") +
    field("Owner", '<select id="f-owner_id">' + ownerOpts + "</select>") +
    field("Next action due", '<input type="date" id="f-due_date" value="' + esc(c.due_date) + '">') +
    field("Hard deadline", '<input type="date" id="f-deadline" value="' + esc(c.deadline) + '">') +
    field("Source", '<input id="f-source" value="' + esc(c.source) + '" placeholder="who asked / where it came from">') +
    field("Workpaper location", '<input id="f-location" value="' + esc(c.location) + '" placeholder="path or link to files">') +
    field("Tags", '<input id="f-tags" value="' + esc(c.tags) + '" placeholder="comma, separated">') +
    field("Description", '<textarea id="f-description">' + esc(c.description) + "</textarea>") +
    '<div class="savebar"><button class="btn primary" id="save-fields">Save</button>' +
    '<span class="t-faint">Fields also save on their own when you leave them.</span></div>' +
    "</div>" +
    '<div class="timeline">' +
    '<div class="rtabs">' +
    '<button class="rtab' + (CASE_RTAB === "activity" ? " on" : "") + '" data-rtab="activity">Activity</button>' +
    '<button class="rtab' + (CASE_RTAB === "checklist" ? " on" : "") + '" data-rtab="checklist">Checklist <span class="count">' +
    (d.checklist || []).length + "</span></button>" +
    "</div>" +
    '<div data-rpane="activity"' + (CASE_RTAB === "activity" ? "" : ' style="display:none"') + ">" +
    '<div class="composer"><textarea id="note-text" placeholder="Add a note — what happened, what you decided, why it moved"></textarea>' +
    '<button class="btn" id="note-add">Add note</button></div>' +
    '<div id="tl">' +
    d.events.slice().reverse().map(e =>
      '<div class="tl-item"><div class="tl-when">' + fmtWhen(e.at) + "</div>" +
      '<div class="tl-body"><span class="tl-actor">' + esc(e.actor) + "</span>" +
      '<span class="tl-kind">' + esc(e.kind.replace("_", " ").toLowerCase()) + "</span>" +
      '<div class="tl-text">' + eventText(e) + "</div></div></div>").join("") +
    "</div></div>" +
    '<div data-rpane="checklist"' + (CASE_RTAB === "checklist" ? "" : ' style="display:none"') + ">" +
    '<div class="chk-sec" style="padding-top:4px">' +
    ((d.checklist || []).map(it => {
      const slug = it.state.toLowerCase().replace(/[^a-z]/g, "");
      const pill = canAct
        ? '<button class="chk-state s-' + slug + '" data-chk="' + it.id + '" data-state="' + esc(it.state) + '" title="Click to advance">' + esc(it.state) + "</button>"
        : '<span class="chk-state s-' + slug + '">' + esc(it.state) + "</span>";
      return '<div class="chk-item">' + pill +
        '<span class="grow' + (it.state === "Received" ? " chk-done" : "") + '">' + esc(it.label) + "</span>" +
        (canAct ? '<button class="chk-del" data-chk-del="' + it.id + '" title="Remove">×</button>' : "") +
        "</div>";
    }).join("") || '<div class="empty" style="padding:6px 0">Nothing to collect yet.</div>') +
    (canAct
      ? '<div class="addline"><input id="chk-new" placeholder="Add an item — e.g. Trade blotter Q1">' +
        '<button class="btn" id="chk-add">Add</button></div>'
      : "") +
    (c.obligation_id
      ? '<div class="t-faint" style="font-size:12px;padding-top:8px">This case came from a recurring item — next year’s copy will start with the same checklist.</div>'
      : "") +
    "</div></div>" +
    "</div></div></div>";

  function field(label, control) {
    return '<div class="f"><label>' + esc(label) + "</label>" + control + "</div>";
  }

  const reload = () => viewCase(no);

  document.querySelectorAll(".statusline .actions button").forEach(b => {
    b.onclick = async () => {
      const to = b.dataset.to;
      if (to === "Waiting") return inlineWaiting();
      if (to === "Cancelled") return inlineCancel();
      try {
        await api("/api/cases/" + encodeURIComponent(no) + "/status", { method: "POST", body: { to } });
        toast(to === "Completed" ? "Completed — auto-archives in 7 days" : "Moved to " + to);
        reload();
      } catch (err) { toast(err.message, true); }
    };
  });

  function inlineWaiting() {
    const opts = BOOT.waiting_options.map(o => "<option>" + esc(o) + "</option>").join("");
    document.getElementById("inline-form").innerHTML =
      '<div class="inlineform"><span class="t-muted">Waiting on</span>' +
      '<select id="w-on">' + opts + "</select>" +
      '<input id="w-detail" placeholder="detail (optional)" style="width:260px">' +
      '<button class="btn primary" id="w-go">Mark waiting</button>' +
      '<button class="btn quiet" id="w-x">Never mind</button></div>';
    document.getElementById("w-x").onclick = () => (document.getElementById("inline-form").innerHTML = "");
    document.getElementById("w-go").onclick = async () => {
      try {
        await api("/api/cases/" + encodeURIComponent(no) + "/status", {
          method: "POST",
          body: { to: "Waiting", waiting_on: document.getElementById("w-on").value, waiting_detail: document.getElementById("w-detail").value },
        });
        reload();
      } catch (err) { toast(err.message, true); }
    };
  }

  function inlineCancel() {
    document.getElementById("inline-form").innerHTML =
      '<div class="inlineform"><span class="t-muted">Reason for cancelling</span>' +
      '<input id="x-reason" style="width:320px">' +
      '<button class="btn danger" id="x-go">Cancel case</button>' +
      '<button class="btn quiet" id="x-x">Never mind</button></div>';
    document.getElementById("x-x").onclick = () => (document.getElementById("inline-form").innerHTML = "");
    document.getElementById("x-go").onclick = async () => {
      try {
        await api("/api/cases/" + encodeURIComponent(no) + "/status", {
          method: "POST",
          body: { to: "Cancelled", reason: document.getElementById("x-reason").value },
        });
        reload();
      } catch (err) { toast(err.message, true); }
    };
  }

  const claim = document.getElementById("claim");
  if (claim) claim.onclick = async () => {
    try {
      await api("/api/cases/" + encodeURIComponent(no) + "/claim", { method: "POST" });
      toast("It's yours");
      reload();
    } catch (err) { toast(err.message, true); }
  };

  const endCover = document.getElementById("end-cover");
  if (endCover) endCover.onclick = async () => {
    try {
      await api("/api/cases/" + encodeURIComponent(no) + "/coverage/end", { method: "POST" });
      toast("Coverage ended");
      reload();
    } catch (err) { toast(err.message, true); }
  };

  document.querySelectorAll("[data-req]").forEach(b => { b.onclick = () => inlineRequest(b.dataset.req); });
  const resolveReq = async (id, decision) => {
    try {
      await api("/api/requests/" + id + "/resolve", { method: "POST", body: { decision } });
      toast(decision === "ACCEPTED" ? "Accepted" : "Declined");
      reload();
    } catch (err) { toast(err.message, true); }
  };
  document.querySelectorAll("[data-rq-accept]").forEach(b => { b.onclick = () => resolveReq(b.dataset.rqAccept, "ACCEPTED"); });
  document.querySelectorAll("[data-rq-decline]").forEach(b => { b.onclick = () => resolveReq(b.dataset.rqDecline, "DECLINED"); });
  document.querySelectorAll("[data-rq-withdraw]").forEach(b => {
    b.onclick = async () => {
      try {
        await api("/api/requests/" + b.dataset.rqWithdraw + "/withdraw", { method: "POST", body: {} });
        toast("Withdrawn");
        reload();
      } catch (err) { toast(err.message, true); }
    };
  });

  function inlineRequest(kind) {
    const needsTo = kind !== "TRANSFER";
    const opts = BOOT.members.filter(m => m.active && m.id !== ME).map(m =>
      '<option value="' + m.id + '">' + esc(m.name) + "</option>").join("");
    const label = kind === "COVERAGE" ? "Ask to cover" : kind === "HELP" ? "Ask for help" :
      "Request transfer from " + esc(c.owner_name);
    const ph = kind === "COVERAGE" ? "e.g. out next week — please watch this one" :
      kind === "TRANSFER" ? "why ownership should move" : "what you need";
    document.getElementById("inline-form").innerHTML =
      '<div class="inlineform"><span class="t-muted">' + label + "</span>" +
      (needsTo ? '<select id="r-to">' + opts + "</select>" : "") +
      '<input id="r-msg" placeholder="' + ph + '" style="width:300px">' +
      '<button class="btn primary" id="r-go">Send</button>' +
      '<button class="btn quiet" id="r-x">Never mind</button></div>';
    document.getElementById("r-x").onclick = () => (document.getElementById("inline-form").innerHTML = "");
    document.getElementById("r-go").onclick = async () => {
      const body = { kind, message: document.getElementById("r-msg").value.trim() };
      if (needsTo) body.to_id = parseInt(document.getElementById("r-to").value, 10);
      try {
        await api("/api/cases/" + encodeURIComponent(no) + "/requests", { method: "POST", body });
        toast("Request sent");
        reload();
      } catch (err) { toast(err.message, true); }
    };
  }

  const fieldKeys = ["title", "type_id", "owner_id", "due_date", "deadline", "source", "location", "tags", "description"];
  const fieldValue = key => {
    const el = document.getElementById("f-" + key);
    if (!el) return null;
    if (key === "type_id" || key === "owner_id") return parseInt(el.value, 10) || 0;
    return el.value.trim();
  };
  const collectDiffs = () => {
    const body = {};
    fieldKeys.forEach(key => {
      const val = fieldValue(key);
      if (val === null) return;
      if (key === "title" && !val) return;
      if (String(val) !== String(c[key])) body[key] = val;
    });
    return body;
  };
  const saveField = async key => {
    const val = fieldValue(key);
    if (val === null || String(val) === String(c[key])) return;
    if (key === "title" && !val) { document.getElementById("f-title").value = c.title; toast("title cannot be empty", true); return; }
    try {
      const r = await api("/api/cases/" + encodeURIComponent(no), { method: "PATCH", body: { [key]: val } });
      Object.assign(c, r.case);
      toast("Saved");
      if (key === "owner_id") reload();
    } catch (err) { toast(err.message, true); reload(); }
  };
  fieldKeys.forEach(key => {
    const el = document.getElementById("f-" + key);
    if (!el) return;
    const evt = (el.tagName === "SELECT" || el.type === "date") ? "change" : "blur";
    el.addEventListener(evt, () => saveField(key));
  });
  const saveBtn = document.getElementById("save-fields");
  if (saveBtn) saveBtn.onclick = async () => {
    if (document.activeElement && document.activeElement.blur) document.activeElement.blur();
    saveBtn.disabled = true;
    try {
      const body = collectDiffs();
      if (!Object.keys(body).length) {
        toast("All changes already saved");
      } else {
        const r = await api("/api/cases/" + encodeURIComponent(no), { method: "PATCH", body });
        Object.assign(c, r.case);
        toast("Saved");
        if ("owner_id" in body) { reload(); return; }
      }
    } catch (err) { toast(err.message, true); }
    saveBtn.disabled = false;
  };

  document.querySelectorAll(".rtab").forEach(b => {
    b.onclick = () => {
      CASE_RTAB = b.dataset.rtab;
      document.querySelectorAll(".rtab").forEach(x => x.classList.toggle("on", x.dataset.rtab === CASE_RTAB));
      document.querySelectorAll("[data-rpane]").forEach(p => { p.style.display = p.dataset.rpane === CASE_RTAB ? "" : "none"; });
    };
  });

  const mkrec = document.querySelector("[data-mkrec]");
  if (mkrec) mkrec.onclick = () => {
    const base = c.due_date || todayStr();
    document.getElementById("inline-form").innerHTML =
      '<div class="inlineform"><span class="t-muted">Repeats</span>' +
      '<select id="mr-freq"><option>Yearly</option><option>Quarterly</option><option>Monthly</option><option>Weekly</option></select>' +
      '<span class="t-muted">next occurrence due</span><input type="date" id="mr-due">' +
      '<span class="t-muted">create</span><input type="number" id="mr-lead" value="30" min="0" max="365" style="width:60px">' +
      '<span class="t-muted">days ahead</span>' +
      '<button class="btn primary" id="mr-go">Make recurring</button>' +
      '<button class="btn quiet" id="mr-x">Never mind</button></div>';
    const dueEl = document.getElementById("mr-due");
    const setDefault = () => {
      const f = document.getElementById("mr-freq").value;
      const t = new Date(base);
      if (f === "Yearly") t.setFullYear(t.getFullYear() + 1);
      else if (f === "Quarterly") t.setMonth(t.getMonth() + 3);
      else if (f === "Monthly") t.setMonth(t.getMonth() + 1);
      else t.setDate(t.getDate() + 7);
      dueEl.value = t.toISOString().slice(0, 10);
    };
    setDefault();
    document.getElementById("mr-freq").addEventListener("change", setDefault);
    document.getElementById("mr-x").onclick = () => (document.getElementById("inline-form").innerHTML = "");
    document.getElementById("mr-go").onclick = async () => {
      try {
        await api("/api/cases/" + encodeURIComponent(no) + "/make-recurring", {
          method: "POST",
          body: {
            frequency: document.getElementById("mr-freq").value,
            next_due: dueEl.value,
            lead_days: parseInt(document.getElementById("mr-lead").value, 10) || 0,
          },
        });
        toast("Recurring item created — see the Recurring tab");
        reload();
      } catch (err) { toast(err.message, true); }
    };
  };

  const CHK_NEXT = { "Needed": "Requested", "Requested": "Received", "Received": "N/A", "N/A": "Needed" };
  document.querySelectorAll("[data-chk]").forEach(b => {
    b.onclick = async () => {
      try {
        await api("/api/checklist/" + b.dataset.chk, { method: "PATCH", body: { state: CHK_NEXT[b.dataset.state] || "Needed" } });
        reload();
      } catch (err) { toast(err.message, true); }
    };
  });
  document.querySelectorAll("[data-chk-del]").forEach(b => {
    b.onclick = async () => {
      try {
        await api("/api/checklist/" + b.dataset.chkDel, { method: "DELETE", body: {} });
        reload();
      } catch (err) { toast(err.message, true); }
    };
  });
  const chkAdd = document.getElementById("chk-add");
  if (chkAdd) {
    const submit = async () => {
      const label = document.getElementById("chk-new").value.trim();
      if (!label) return;
      try {
        await api("/api/cases/" + encodeURIComponent(no) + "/checklist", { method: "POST", body: { label } });
        reload();
      } catch (err) { toast(err.message, true); }
    };
    chkAdd.onclick = submit;
    document.getElementById("chk-new").addEventListener("keydown", e => { if (e.key === "Enter") submit(); });
  }

  document.getElementById("note-add").onclick = async () => {
    const text = document.getElementById("note-text").value.trim();
    if (!text) return;
    try {
      await api("/api/cases/" + encodeURIComponent(no) + "/notes", { method: "POST", body: { text } });
      reload();
    } catch (err) { toast(err.message, true); }
  };
}

// ---------- recurring ----------

async function viewRecurring() {
  view().innerHTML = '<div class="page"><div class="empty">Loading…</div></div>';
  let d;
  try { d = await api("/api/obligations"); } catch (err) {
    view().innerHTML = '<div class="page"><div class="empty">' + esc(err.message) + "</div></div>";
    return;
  }
  const rows = d.obligations.map(o =>
    '<tr' + (o.active ? "" : ' style="opacity:.55"') + ">" +
    "<td>" + esc(o.name) + (o.active ? "" : ' <span class="t-faint">· paused</span>') + "</td>" +
    '<td class="t-muted">' + (esc(o.type_name) || '<span class="t-faint">—</span>') + "</td>" +
    '<td class="t-muted">' + esc(o.frequency) + "</td>" +
    '<td class="c-due">' + fmtDate(o.next_due) + "</td>" +
    '<td class="t-muted">' + o.lead_days + "d ahead</td>" +
    '<td class="t-muted">' + (esc(o.owner_name) || '<span class="t-faint">unassigned</span>') + "</td>" +
    '<td class="t-muted">' + o.checklist.length + " items · " + o.case_count + " cases</td>" +
    "<td>" +
    (o.active ? '<button class="btn quiet" data-sp="' + o.id + '">Create now</button>' : "") +
    '<button class="btn quiet" data-tg="' + o.id + '" data-on="' + (!o.active) + '">' + (o.active ? "Pause" : "Resume") + "</button>" +
    "</td></tr>").join("");
  view().innerHTML =
    '<div class="page page-wide">' +
    "<h1>Recurring</h1>" +
    '<p class="t-muted" style="font-size:13px;margin:6px 0 0">Each recurring item opens a fresh case ahead of its deadline — same type, same owner, same checklist. Finish this year, and next year shows up by itself.</p>' +
    '<div class="sec"><div class="sec-label">Items <span class="count">' + d.obligations.length + "</span></div>" +
    (d.obligations.length
      ? '<table class="list"><thead><tr><th>Name</th><th>Type</th><th>Repeats</th><th>Next due</th><th>Created</th><th>Owner</th><th>Checklist</th><th></th></tr></thead><tbody>' +
        rows + "</tbody></table>"
      : '<div class="empty">Nothing recurring yet. Add one below, or open any case and press “Make recurring…”.</div>') +
    "</div>" +
    '<div class="sec"><div class="sec-label">New recurring item</div>' +
    '<div class="recform">' +
    '<div class="f"><label>Name</label><input id="ob-name" placeholder="e.g. Annual CEO certification"></div>' +
    '<div class="recrow">' +
    '<div class="f"><label>Type</label><select id="ob-type"><option value="0">—</option>' +
    BOOT.types.filter(t => t.active).map(t => '<option value="' + t.id + '">' + esc(t.name) + "</option>").join("") +
    "</select></div>" +
    '<div class="f"><label>Owner</label><select id="ob-owner"><option value="0">Unassigned</option>' +
    BOOT.members.filter(m => m.active).map(m => '<option value="' + m.id + '"' + (m.id === ME ? " selected" : "") + ">" + esc(m.name) + "</option>").join("") +
    "</select></div>" +
    '<div class="f"><label>Repeats</label><select id="ob-freq"><option>Yearly</option><option>Quarterly</option><option>Monthly</option><option>Weekly</option></select></div>' +
    '<div class="f"><label>First due date</label><input type="date" id="ob-due"></div>' +
    '<div class="f"><label>Create days ahead</label><input type="number" id="ob-lead" value="30" min="0" max="365"></div>' +
    "</div>" +
    '<div class="f"><label>Checklist template — one item per line, copied into every new case</label>' +
    '<textarea id="ob-chk" rows="3" placeholder="Trade blotter&#10;Approval emails&#10;Signed certification"></textarea></div>' +
    '<button class="btn primary" id="ob-add">Add recurring item</button>' +
    "</div></div></div>";
  document.querySelectorAll("[data-sp]").forEach(b => {
    b.onclick = async () => {
      try {
        const r = await api("/api/obligations/" + b.dataset.sp + "/spawn", { method: "POST", body: {} });
        toast(r.case_no + " created");
        viewRecurring();
      } catch (err) { toast(err.message, true); }
    };
  });
  document.querySelectorAll("[data-tg]").forEach(b => {
    b.onclick = async () => {
      try {
        await api("/api/obligations/" + b.dataset.tg, { method: "PATCH", body: { active: b.dataset.on === "true" } });
        viewRecurring();
      } catch (err) { toast(err.message, true); }
    };
  });
  document.getElementById("ob-add").onclick = async () => {
    try {
      await api("/api/obligations", {
        method: "POST",
        body: {
          name: document.getElementById("ob-name").value.trim(),
          type_id: parseInt(document.getElementById("ob-type").value, 10) || 0,
          owner_id: parseInt(document.getElementById("ob-owner").value, 10) || 0,
          frequency: document.getElementById("ob-freq").value,
          next_due: document.getElementById("ob-due").value,
          lead_days: parseInt(document.getElementById("ob-lead").value, 10) || 0,
          checklist: document.getElementById("ob-chk").value.split("\n"),
        },
      });
      toast("Recurring item added");
      viewRecurring();
    } catch (err) { toast(err.message, true); }
  };
}

// ---------- calendar ----------

function identityQS() {
  const pin = localStorage.getItem("cb_pin") || "";
  return "member=" + ME + (pin ? "&pin=" + encodeURIComponent(pin) : "");
}

let CAL_MONTH = null; // {y, m} 0-based month
let CAL_SELECTED = null;

async function viewCalendar() {
  view().innerHTML = '<div class="page page-wide"><div class="empty">Loading…</div></div>';
  let cases, obs;
  try {
    cases = (await api("/api/cases")).cases.filter(c => c.due_date || c.deadline);
    obs = (await api("/api/obligations")).obligations.filter(o => o.active);
  } catch (err) {
    view().innerHTML = '<div class="page"><div class="empty">' + esc(err.message) + "</div></div>";
    return;
  }
  const td = todayStr();
  if (!CAL_MONTH) { const n = new Date(); CAL_MONTH = { y: n.getFullYear(), m: n.getMonth() }; }

  // index everything by date
  const items = {}; // date -> [{kind,label,case_no,cls}]
  const add = (date, it) => { if (date) (items[date] = items[date] || []).push(it); };
  cases.forEach(c => {
    const over = c.due_date && c.due_date < td && !["Completed", "Archived", "Cancelled"].includes(c.status);
    const done = ["Completed", "Archived", "Cancelled"].includes(c.status);
    add(c.due_date, { case_no: c.case_no, title: c.title, owner: c.owner_name, cls: done ? "done" : over ? "over" : "", label: c.title });
    if (c.deadline && c.deadline !== c.due_date) {
      add(c.deadline, { case_no: c.case_no, title: c.title, owner: c.owner_name, cls: "hard", label: "⚑ " + c.title });
    }
  });
  obs.forEach(o => add(o.next_due, { rec: true, title: o.name, cls: "rec", label: "↻ " + o.name }));

  const icsUrl = location.origin + "/api/calendar.ics?" + identityQS();
  view().innerHTML =
    '<div class="page page-wide">' +
    '<div style="display:flex;align-items:baseline;gap:14px;flex-wrap:wrap"><h1>Calendar</h1>' +
    '<span class="t-faint" style="font-size:12.5px">Subscribe in Outlook: <span class="mono" style="user-select:all">' + esc(icsUrl) + "</span> " +
    '<button class="btn quiet" id="copy-ics">Copy link</button></span></div>' +
    '<div id="cal-mount"></div>' +
    '<div id="cal-day"></div>' +
    "</div>";

  const copyBtn = document.getElementById("copy-ics");
  if (copyBtn) copyBtn.onclick = async () => {
    try { await navigator.clipboard.writeText(icsUrl); toast("Link copied"); }
    catch (_) { toast("Copy failed — select the link text and copy it", true); }
  };

  function renderGrid() {
    const { y, m } = CAL_MONTH;
    const monthName = new Date(y, m, 1).toLocaleString("en-US", { month: "long", year: "numeric" });
    const first = new Date(y, m, 1);
    const startDow = first.getDay();
    const daysInMonth = new Date(y, m + 1, 0).getDate();
    const dows = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
    let cells = "";
    const cellFor = (dateObj, muted) => {
      const ds = dateObj.getFullYear() + "-" + String(dateObj.getMonth() + 1).padStart(2, "0") + "-" + String(dateObj.getDate()).padStart(2, "0");
      const list = items[ds] || [];
      const isToday = ds === td;
      const sel = ds === CAL_SELECTED;
      const shown = list.slice(0, 3).map(it =>
        '<span class="cal-pill ' + it.cls + '" title="' + esc(it.title) + '">' + esc(it.label) + "</span>").join("");
      const more = list.length > 3 ? '<span class="cal-more">+' + (list.length - 3) + " more</span>" : "";
      return '<div class="calcell' + (muted ? " muted" : "") + (isToday ? " today" : "") + (sel ? " selected" : "") +
        '" data-date="' + ds + '"><span class="daynum">' + dateObj.getDate() + "</span>" + shown + more + "</div>";
    };
    for (let i = 0; i < startDow; i++) {
      const dd = new Date(y, m, 1 - (startDow - i));
      cells += cellFor(dd, true);
    }
    for (let day = 1; day <= daysInMonth; day++) cells += cellFor(new Date(y, m, day), false);
    const totalSoFar = startDow + daysInMonth;
    const trail = (7 - (totalSoFar % 7)) % 7;
    for (let i = 1; i <= trail; i++) cells += cellFor(new Date(y, m + 1, i), true);

    document.getElementById("cal-mount").innerHTML =
      '<div class="calbar"><h2>' + monthName + "</h2>" +
      '<button class="nav-btn" id="cal-prev" aria-label="Previous month">‹</button>' +
      '<button class="nav-btn" id="cal-next" aria-label="Next month">›</button>' +
      '<button class="btn quiet" id="cal-today">Today</button></div>' +
      '<div class="calgrid">' + dows.map(d => '<div class="dow">' + d + "</div>").join("") + cells + "</div>";

    document.getElementById("cal-prev").onclick = () => { CAL_MONTH = shiftMonth(CAL_MONTH, -1); renderGrid(); };
    document.getElementById("cal-next").onclick = () => { CAL_MONTH = shiftMonth(CAL_MONTH, 1); renderGrid(); };
    document.getElementById("cal-today").onclick = () => { const n = new Date(); CAL_MONTH = { y: n.getFullYear(), m: n.getMonth() }; CAL_SELECTED = td; renderGrid(); renderDay(); };
    document.querySelectorAll(".calcell").forEach(c => {
      c.onclick = () => { CAL_SELECTED = c.dataset.date; renderGrid(); renderDay(); };
    });
  }

  function renderDay() {
    const el = document.getElementById("cal-day");
    if (!CAL_SELECTED) { el.innerHTML = ""; return; }
    const list = items[CAL_SELECTED] || [];
    el.innerHTML = '<div class="sec"><div class="sec-label">' + fmtDate(CAL_SELECTED) +
      (CAL_SELECTED === td ? " — today" : "") + ' <span class="count">' + list.length + "</span></div>" +
      (list.length === 0 ? '<div class="empty">Nothing on this day.</div>' :
        list.map(it => '<div class="cal-row">' +
          (it.rec ? '<span class="t-muted">↻ ' + esc(it.title) + " (recurring item)</span>" :
            '<a href="#/case/' + encodeURIComponent(it.case_no) + '" class="mono">' + esc(it.case_no) + "</a> " + esc(it.title) +
            (it.cls === "hard" ? ' <span style="color:var(--red);font-size:11px;font-weight:500">HARD DEADLINE</span>' : "") +
            ' <span class="t-faint">· ' + (esc(it.owner) || "unassigned") + "</span>") +
          "</div>").join("")) + "</div>";
  }

  renderGrid();
  renderDay();
}

function shiftMonth(cm, delta) {
  const d = new Date(cm.y, cm.m + delta, 1);
  return { y: d.getFullYear(), m: d.getMonth() };
}

// ---------- reports ----------

async function viewReports() {
  view().innerHTML = '<div class="page"><div class="empty">Loading…</div></div>';
  let d;
  try { d = await api("/api/reports"); } catch (err) {
    view().innerHTML = '<div class="page"><div class="empty">' + esc(err.message) + "</div></div>";
    return;
  }
  const groups = {};
  d.waiting.forEach(c => { (groups[c.waiting_on || "Other"] = groups[c.waiting_on || "Other"] || []).push(c); });
  const groupNames = Object.keys(groups).sort((a, b) => groups[b].length - groups[a].length);
  const agingHtml = groupNames.length === 0
    ? '<div class="empty">Nothing is waiting on anyone.</div>'
    : groupNames.map(g => {
        const list = groups[g].slice().sort((a, b) => daysSince(b.waiting_since) - daysSince(a.waiting_since));
        const oldest = daysSince(list[0].waiting_since);
        return '<div class="sec-label" style="margin-top:14px">' + esc(g) +
          ' <span class="count">' + list.length + (list.length === 1 ? " case" : " cases") + ", oldest " + oldest + "d</span></div>" +
          list.map(c => '<div class="cal-row"><a href="#/case/' + encodeURIComponent(c.case_no) + '" class="mono">' +
            esc(c.case_no) + "</a> " + esc(c.title) +
            ' <span class="t-faint">· ' + esc(c.owner_name) + " · waiting " + daysSince(c.waiting_since) + "d" +
            (c.waiting_detail ? " — " + esc(c.waiting_detail) : "") + "</span></div>").join("");
      }).join("");
  const wl = d.workload.map(l =>
    "<tr><td>" + esc(l.member) + '</td><td class="num">' + l.open + '</td><td class="num">' + l.in_progress +
    '</td><td class="num">' + l.waiting + '</td><td class="num"' + (l.overdue ? ' style="color:var(--red);font-weight:500"' : "") + ">" +
    l.overdue + "</td></tr>").join("");
  view().innerHTML =
    '<div class="page">' +
    "<h1>Reports</h1>" +
    '<div class="sec"><div class="sec-label">Stuck — by who we are waiting on</div>' + agingHtml + "</div>" +
    '<div class="sec"><div class="sec-label">Workload</div>' +
    '<table class="list"><thead><tr><th>Member</th><th class="num">Open</th><th class="num">In progress</th>' +
    '<th class="num">Waiting</th><th class="num">Overdue</th></tr></thead><tbody>' + wl + "</tbody></table></div>" +
    '<div class="sec"><div class="sec-label">Overdue <span class="count">' + d.overdue.length + "</span></div>" +
    caseTable(d.overdue, { empty: "Nothing is overdue." }) + "</div>" +
    "</div>";
  bindRows(view());
}

// ---------- import ----------

async function viewImport() {
  view().innerHTML =
    '<div class="page">' +
    "<h1>Import</h1>" +
    '<p class="t-muted" style="font-size:13px;margin:6px 0 0">Already tracking work in a spreadsheet? ' +
    "Download the template, fill it in, upload it back. Nothing is created until every row passes validation.</p>" +
    '<div class="sec"><div class="sec-label">Step 1 — template</div>' +
    '<div style="padding:12px 0"><a class="btn" href="/api/import/template" download>Download template (.xlsx)</a></div></div>' +
    '<div class="sec"><div class="sec-label">Step 2 — upload and validate</div>' +
    '<div style="padding:12px 0;display:flex;gap:12px;align-items:center;flex-wrap:wrap">' +
    '<input type="file" id="imp-file" accept=".xlsx">' +
    '<button class="btn primary" id="imp-check">Validate</button></div>' +
    '<div id="imp-result"></div></div>' +
    "</div>";
  const upload = async commit => {
    const input = document.getElementById("imp-file");
    if (!input.files.length) { toast("Pick the filled-in template first", true); return; }
    const fd = new FormData();
    fd.append("file", input.files[0]);
    const headers = { "X-Member": String(ME) };
    const pin = localStorage.getItem("cb_pin");
    if (pin) headers["X-Pin"] = pin;
    let res, data;
    try {
      res = await fetch("/api/import" + (commit ? "?commit=1" : ""), { method: "POST", headers, body: fd });
      data = await res.json();
    } catch (err) { toast("Upload failed: " + err.message, true); return; }
    if (!res.ok) { toast(data.error || "Upload failed", true); return; }
    const out = document.getElementById("imp-result");
    if (data.committed) {
      out.innerHTML = '<div class="sec-label" style="color:var(--green-ink)">Imported ' + data.created.length +
        ' cases</div><div style="padding:10px 0"><a href="#/cases" class="btn">Open Cases</a></div>';
      toast(data.created.length + " cases imported");
      return;
    }
    const bad = data.error_count > 0;
    out.innerHTML =
      '<div class="sec-label"' + (bad ? ' style="color:var(--red)"' : "") + ">" +
      (bad ? data.error_count + " problems found — fix the file and validate again" : data.rows.length + " rows ready — nothing imported yet") +
      "</div>" +
      '<table class="list"><thead><tr><th style="width:50px">Row</th><th>Title</th><th>Owner</th><th>Status</th><th>Due</th><th>Problems</th></tr></thead><tbody>' +
      data.rows.map(rw =>
        "<tr" + (rw.errors.length ? ' style="background:#FDF3F2"' : "") + '><td class="t-muted">' + rw.row_no + "</td>" +
        "<td>" + esc(rw.data.title) + "</td><td>" + (esc(rw.data.owner) || '<span class="t-faint">unassigned</span>') + "</td>" +
        "<td>" + esc(rw.data.status) + "</td><td>" + esc(rw.data.due_date) + "</td>" +
        '<td style="color:var(--red)">' + rw.errors.map(esc).join("<br>") + "</td></tr>").join("") +
      "</tbody></table>" +
      (bad ? "" : '<div style="padding:14px 0"><button class="btn primary" id="imp-commit">Import ' + data.rows.length + " cases</button></div>");
    const commitBtn = document.getElementById("imp-commit");
    if (commitBtn) commitBtn.onclick = () => upload(true);
  };
  document.getElementById("imp-check").onclick = () => upload(false);
}

// ---------- inbox ----------

async function viewInbox() {
  view().innerHTML = '<div class="page"><div class="empty">Loading…</div></div>';
  let d;
  try { d = await api("/api/inbox"); } catch (err) {
    view().innerHTML = '<div class="page"><div class="empty">' + esc(err.message) + "</div></div>";
    return;
  }
  const item = (rq, mode) => {
    let btns = "";
    if (mode === "in") {
      btns = '<button class="btn primary" data-rq-accept="' + rq.id + '">Accept</button> ' +
        '<button class="btn" data-rq-decline="' + rq.id + '">Decline</button>';
    } else if (mode === "out") {
      btns = '<button class="btn quiet" data-rq-withdraw="' + rq.id + '">Withdraw</button>';
    } else {
      btns = '<span class="t-faint">' + esc(rq.status.toLowerCase()) + "</span>";
    }
    return '<div class="req-item">' +
      '<a href="#/case/' + encodeURIComponent(rq.case_no) + '" class="mono">' + esc(rq.case_no) + "</a>" +
      '<span class="req-kind">' + esc(rq.kind.toLowerCase()) + "</span>" +
      '<span class="grow">' + esc(rq.case_title) +
      ' <span class="t-muted">· ' + (mode === "in" ? "from <b>" + esc(rq.from_name) + "</b>" : "to <b>" + esc(rq.to_name) + "</b>") +
      (rq.message ? " — " + esc(rq.message) : "") + "</span></span>" +
      '<span class="req-actions">' + btns + "</span></div>";
  };
  const sec = (label, rows, mode, empty) =>
    '<div class="sec"><div class="sec-label">' + label + ' <span class="count">' + rows.length + "</span></div>" +
    (rows.length ? rows.map(rq => item(rq, mode)).join("") : '<div class="empty">' + empty + "</div>") + "</div>";
  view().innerHTML =
    '<div class="page">' +
    "<h1>Inbox</h1>" +
    sec("Needs your decision", d.incoming, "in", "Nothing waiting on you.") +
    sec("Sent by you, still open", d.outgoing, "out", "No open requests from you.") +
    sec("Recently resolved", d.recent, "done", "No history yet.") +
    "</div>";
  const refresh = () => { viewInbox(); updateInboxBadge(); };
  document.querySelectorAll("[data-rq-accept]").forEach(b => {
    b.onclick = async () => {
      try {
        await api("/api/requests/" + b.dataset.rqAccept + "/resolve", { method: "POST", body: { decision: "ACCEPTED" } });
        toast("Accepted");
        refresh();
      } catch (err) { toast(err.message, true); }
    };
  });
  document.querySelectorAll("[data-rq-decline]").forEach(b => {
    b.onclick = async () => {
      try {
        await api("/api/requests/" + b.dataset.rqDecline + "/resolve", { method: "POST", body: { decision: "DECLINED" } });
        toast("Declined");
        refresh();
      } catch (err) { toast(err.message, true); }
    };
  });
  document.querySelectorAll("[data-rq-withdraw]").forEach(b => {
    b.onclick = async () => {
      try {
        await api("/api/requests/" + b.dataset.rqWithdraw + "/withdraw", { method: "POST", body: {} });
        toast("Withdrawn");
        refresh();
      } catch (err) { toast(err.message, true); }
    };
  });
}

// ---------- settings ----------

async function viewSettings() {
  const meIsAdmin = (BOOT.members.find(m => m.id === ME) || {}).is_admin;
  const memberRows = BOOT.members.map(m => {
    let pinCtl = "";
    if (m.id === ME) {
      pinCtl = '<button class="btn quiet" data-pin-self="1">' + (m.has_pin ? "Change PIN" : "Set PIN") + "</button>" +
        (m.has_pin ? '<button class="btn quiet" data-pin-remove="1">Remove PIN</button>' : "");
    } else if (meIsAdmin && m.has_pin) {
      pinCtl = '<button class="btn quiet" data-pin-clear="' + m.id + '">Clear PIN</button>';
    }
    return '<div class="row"><span class="grow">' + esc(m.name) +
      (m.is_admin ? ' <span class="t-faint">· set up this workspace</span>' : "") +
      (m.has_pin ? ' <span class="t-faint">· PIN set</span>' : "") +
      (m.active ? "" : ' <span class="t-faint">· deactivated</span>') + "</span>" +
      pinCtl +
      '<button class="btn quiet" data-id="' + m.id + '" data-active="' + (!m.active) + '">' +
      (m.active ? "Deactivate" : "Reactivate") + "</button></div>";
  }).join("");
  const typeRows = BOOT.types.map(t =>
    '<div class="row"><span class="grow">' + esc(t.name) + "</span></div>").join("");
  view().innerHTML =
    '<div class="page settings">' +
    "<h1>Settings</h1>" +
    '<div class="sec"><div class="sec-label">Team</div>' +
    '<div class="row"><span class="grow">' + esc(BOOT.team_name) + "</span></div></div>" +
    '<div class="sec"><div class="sec-label">Members</div>' + memberRows +
    '<div id="pin-form"></div>' +
    '<div class="addline"><input id="nm-name" placeholder="New member name">' +
    '<button class="btn" id="nm-add">Add member</button></div></div>' +
    '<div class="sec"><div class="sec-label">Case types</div>' + typeRows +
    '<div class="addline"><input id="nt-name" placeholder="New case type">' +
    '<button class="btn" id="nt-add">Add type</button></div></div>' +
    '<div class="sec"><div class="sec-label">Data</div>' +
    '<div class="row"><span class="grow">Examiner package — every case, the full audit log, checklists and members, as one workbook</span>' +
    '<a class="btn" href="/api/export/examiner?' + identityQS() + '" download>Download</a></div>' +
    '<div class="row"><span class="grow">Records folder — refresh the human-readable Excel snapshots now</span>' +
    '<button class="btn" id="dt-records">Refresh</button></div>' +
    '<div class="row"><span class="grow">Back up the data file to Backups/ right now (also runs daily by itself)</span>' +
    '<button class="btn" id="dt-backup">Back up now</button></div>' +
    '<div class="row"><span class="grow">Load sample data — a handful of demo cases to look around (near-empty workspace only)</span>' +
    '<button class="btn" id="dt-sample">Load</button></div>' +
    "</div>" +
    "</div>";
  const meRec = BOOT.members.find(m => m.id === ME) || {};
  const savePin = async (id, pin, oldPin) => {
    try {
      const r = await api("/api/members/" + id + "/pin", { method: "POST", body: { pin, old_pin: oldPin } });
      BOOT.members = r.members;
      if (id === ME) {
        if (pin) localStorage.setItem("cb_pin", pin); else localStorage.removeItem("cb_pin");
      }
      toast(pin ? "PIN saved" : "PIN removed");
      viewSettings();
    } catch (err) { toast(err.message, true); }
  };
  const selfBtn = document.querySelector("[data-pin-self]");
  if (selfBtn) selfBtn.onclick = () => {
    document.getElementById("pin-form").innerHTML =
      '<div class="inlineform">' +
      (meRec.has_pin ? '<input id="pf-old" type="password" inputmode="numeric" maxlength="8" placeholder="current PIN" style="width:120px">' : "") +
      '<input id="pf-new" type="password" inputmode="numeric" maxlength="8" placeholder="new PIN (4–8 digits)" style="width:160px">' +
      '<button class="btn primary" id="pf-go">Save PIN</button>' +
      '<button class="btn quiet" id="pf-x">Never mind</button></div>';
    document.getElementById("pf-x").onclick = () => (document.getElementById("pin-form").innerHTML = "");
    document.getElementById("pf-go").onclick = () => {
      const oldEl = document.getElementById("pf-old");
      savePin(ME, document.getElementById("pf-new").value.trim(), oldEl ? oldEl.value.trim() : "");
    };
  };
  const removeBtn = document.querySelector("[data-pin-remove]");
  if (removeBtn) removeBtn.onclick = () => {
    document.getElementById("pin-form").innerHTML =
      '<div class="inlineform"><input id="pf-old" type="password" inputmode="numeric" maxlength="8" placeholder="current PIN" style="width:120px">' +
      '<button class="btn danger" id="pf-go">Remove PIN</button>' +
      '<button class="btn quiet" id="pf-x">Never mind</button></div>';
    document.getElementById("pf-x").onclick = () => (document.getElementById("pin-form").innerHTML = "");
    document.getElementById("pf-go").onclick = () => savePin(ME, "", document.getElementById("pf-old").value.trim());
  };
  document.querySelectorAll("[data-pin-clear]").forEach(b => {
    b.onclick = () => savePin(parseInt(b.dataset.pinClear, 10), "", "");
  });
  document.querySelectorAll(".settings .row button[data-id]").forEach(b => {
    b.onclick = async () => {
      try {
        const r = await api("/api/members/" + b.dataset.id, { method: "PATCH", body: { active: b.dataset.active === "true" } });
        BOOT.members = r.members;
        viewSettings();
      } catch (err) { toast(err.message, true); }
    };
  });
  document.getElementById("nm-add").onclick = async () => {
    const name = document.getElementById("nm-name").value.trim();
    if (!name) return;
    try {
      const r = await api("/api/members", { method: "POST", body: { name } });
      BOOT.members = r.members;
      viewSettings();
      toast(name + " added — they can now pick their name on the join screen");
    } catch (err) { toast(err.message, true); }
  };
  document.getElementById("nt-add").onclick = async () => {
    const name = document.getElementById("nt-name").value.trim();
    if (!name) return;
    try {
      const r = await api("/api/types", { method: "POST", body: { name } });
      BOOT.types = r.types;
      toast("Type added");
      viewSettings();
    } catch (err) { toast(err.message, true); }
  };
  document.getElementById("dt-records").onclick = async () => {
    try {
      const r = await api("/api/records/refresh", { method: "POST", body: {} });
      toast("Records refreshed: " + r.folder);
    } catch (err) { toast(err.message, true); }
  };
  document.getElementById("dt-backup").onclick = async () => {
    try {
      const r = await api("/api/backup", { method: "POST", body: {} });
      toast("Backed up: " + r.file);
    } catch (err) { toast(err.message, true); }
  };
  document.getElementById("dt-sample").onclick = async () => {
    if (!confirm("Load a handful of demo cases into this workspace?")) return;
    try {
      const r = await api("/api/sample", { method: "POST", body: {} });
      toast(r.created + " sample cases created");
      location.hash = "#/today";
    } catch (err) { toast(err.message, true); }
  };
}

init().catch(err => {
  app().innerHTML = '<div class="who"><h1>Cannot reach the workspace</h1><p>' + esc(err.message) + "</p></div>";
});
