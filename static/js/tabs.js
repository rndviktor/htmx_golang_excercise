// -----------------------------------------------------------------------------
// Tabs: open/switch/close script tabs, run queries, output switching and the
// query-history side panel. Global entry points (executeQuery, switchTab,
// goToPage, ...) are consumed by inline onclick handlers in HTML fragments.
// -----------------------------------------------------------------------------

let tabCounter = 0;
let activeTabId = TAB_DASHBOARD;
// Per-tab save state: {name, path, dirty}. `name` is the tab label without
// the unsaved marker; `path` stays empty until the script has been saved.
const tabMeta = {};
let scriptCounter = 0;

// -----------------------------------------------------------------------------
// Output panel helpers
// -----------------------------------------------------------------------------

function setTabBtnActive(btn, active) {
    btn.classList.toggle("bg-gray-900", active);
    btn.classList.toggle("text-blue-400", active);
    btn.classList.toggle("border-t-2", active);
    btn.classList.toggle("border-blue-500", active);
    btn.classList.toggle("text-gray-400", !active);
    btn.classList.toggle("hover:text-gray-200", !active);
}

function setOutputTabActive(t, active) {
    t.classList.toggle("text-blue-400", active);
    t.classList.toggle("font-bold", active);
    t.classList.toggle("border-t-blue-500", active);
    t.classList.toggle("text-gray-400", !active);
    t.classList.toggle("font-normal", !active);
    t.classList.toggle("border-t-transparent", !active);
}

function switchTab(id) {
    document.querySelectorAll(".tab-btn").forEach((btn) => {
        setTabBtnActive(btn, btn.dataset.tabId === id);
    });
    document.querySelectorAll(".tab-panel").forEach((panel) => {
        panel.style.display = panel.id === TAB_CONTENT_PREFIX + id ? "" : "none";
    });
    activeTabId = id;
    updateMonitoringForActiveTab();
    if (historyPanelOpen) {
        loadQueryHistory();
        openHistoryStream();
    }
}

// Monitoring only polls while the Dashboard tab is active. Switching to
// another tab pauses polling; returning to it resumes (if a database or
// deeper node is still the selected tree selection).
function updateMonitoringForActiveTab() {
    if (activeTabId !== TAB_DASHBOARD) {
        if (monitoringTimer) { clearInterval(monitoringTimer); monitoringTimer = null; }
        closeMonitoringKPIStream();
        return;
    }
    if (monitoringConn && monitoringBaseURL) {
        pollMonitoring();
        openMonitoringKPIStream();
    }
}

function switchOutputTab(tab) {
    const panel = tab.closest("[id^='tab-content']");
    if (!panel) return;
    const name = tab.dataset.outputTab;

    panel.querySelectorAll(".output-tab").forEach((t) => {
        setOutputTabActive(t, t.dataset.outputTab === name);
    });
    panel.querySelectorAll(".output-panel").forEach((p) => {
        const isDataGrid = p.id === "data-grid";
        const hide = p.id !== name + "-panel" && !(isDataGrid && name === "data");
        p.classList.toggle("hidden", hide);
    });
}

function logMessage(panel, type, text) {
    const box = panel.querySelector("#messages-panel > div");
    if (!box) return;
    const ts = new Date().toLocaleTimeString();
    const colors = { info: "text-gray-300", success: "text-green-400", error: "text-red-400", warn: "text-yellow-400" };
    const line = document.createElement("div");
    line.className = "py-0.5 " + (colors[type] || "text-gray-300");
    line.textContent = "[" + ts + "] " + text;
    box.appendChild(line);
    box.parentElement.scrollTop = box.parentElement.scrollHeight;
}

function panelTabId(panel) {
    return (panel.id || "").replace(TAB_CONTENT_PREFIX, "");
}

// -----------------------------------------------------------------------------
// Script tab save state (labels + dirty markers)
// -----------------------------------------------------------------------------

function basename(path) {
    const parts = String(path).split(/[\\/]/);
    return parts[parts.length - 1] || path;
}

function displayTabName(id) {
    const m = tabMeta[id];
    if (!m) return "Query";
    return m.name + (m.dirty ? "*" : "");
}

function updateTabLabel(id) {
    const btn = document.querySelector('.tab-btn[data-tab-id="' + id + '"]');
    const span = btn ? btn.querySelector("span") : null;
    const m = tabMeta[id];
    if (span) span.textContent = displayTabName(id);
    if (btn) btn.title = (m && m.path) ? m.path : "";
}

// Marks the tab dirty after the user edits the editor (wired from the
// CodeMirror updateListener; programmatic set()/restore calls are excluded).
function markTabDirty(panel) {
    const id = panelTabId(panel);
    const m = tabMeta[id];
    if (m && !m.dirty) {
        m.dirty = true;
        updateTabLabel(id);
    }
}

function saveScript(btn) {
    const panel = btn.closest("[id^='tab-content']");
    if (!panel) return;
    const id = panelTabId(panel);
    const m = tabMeta[id];
    if (m && m.path) {
        doSaveScript(id, m.path);
    } else {
        showSaveDialog(id);
    }
}

function saveScriptAs(btn) {
    const panel = btn.closest("[id^='tab-content']");
    if (!panel) return;
    showSaveDialog(panelTabId(panel));
}

// Opens the Save As dialog component (static/js/save-dialog.js). The dialog
// loads its markup from the server partial and prefills the path with the
// server process folder; the chosen path is persisted by doSaveScript.
function showSaveDialog(id) {
    const m = tabMeta[id];
    if (!window.SaveDialog) return;
    window.SaveDialog.open({
        id: id,
        name: (m && m.name) ? m.name.replace(/\*$/, "") : "script.sql",
        onSave: (path) => doSaveScript(id, path),
    });
}

function doSaveScript(id, path) {
    const panel = document.getElementById(TAB_CONTENT_PREFIX + id);
    if (!panel) return;
    const content = window.SqlEditor ? window.SqlEditor.value(panel) : "";
    fetch("/api/save-script", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: path, content: content }),
    })
        .then((r) => { if (!r.ok) throw r; return r.json(); })
        .then(() => {
            const m = tabMeta[id];
            if (m) {
                m.path = path;
                m.name = basename(path);
                m.dirty = false;
                updateTabLabel(id);
            }
            logMessage(panel, "info", "Saved to " + path);
        })
        .catch(async (err) => {
            const msg = (err && err.text) ? await err.text() : String(err);
            logMessage(panel, "error", "Save failed: " + msg);
        });
}

// Returns the #query-editor form of a script tab panel, if present.
function queryForm(panel) {
    return panel ? panel.querySelector("#" + ID_QUERY_EDITOR) : null;
}

// Returns the {sid, db} hidden inputs backing a query form, or null when the
// form is not bound to a connection.
function formConnectionParams(form) {
    if (!form) return null;
    const sid = form.querySelector("input[name='" + PARAM_SERVER_ID + "']");
    const db = form.querySelector("input[name='" + PARAM_DB_NAME + "']");
    if (!sid || !db) return null;
    return { sid: sid, db: db };
}

// -----------------------------------------------------------------------------
// Query execution
// -----------------------------------------------------------------------------

// Formats (indents) the SQL in the active script editor: the selection only
// when text is selected, otherwise the whole document.
function formatSql(btn) {
    const panel = btn.closest("[id^='tab-content']");
    if (panel && window.SqlEditor && window.SqlEditor.format) {
        window.SqlEditor.format(panel);
    }
}

function executeQuery(btn, page) {
    const panel = btn.closest("[id^='tab-content']");
    if (!panel) return;
    const form = queryForm(panel);
    const grid = panel.querySelector("#data-grid");
    const status = panel.querySelector(".query-status");
    const pag = panel.querySelector(".pagination-controls");
    const pageInfo = panel.querySelector(".page-info");
    if (!form || !grid) return;

    const params = formConnectionParams(form);
    if (!params) return;

    const ed = window.SqlEditor;
    const doc = ed && ed.view(panel) ? ed.value(panel) : "";
    const sel = ed && ed.view(panel) ? ed.selection(panel) : { from: 0, to: 0 };
    const selStart = sel.from;
    const selEnd = sel.to;
    const hasSelection = selStart !== selEnd;
    const query = hasSelection
        ? doc.substring(selStart, selEnd)
        : doc;
    if (!query) return;

    if (!page) page = 1;

    const body = new URLSearchParams({
        sql_query: query,
        server_id: params.sid.value,
        db_name: params.db.value,
        tab_id: panelTabId(panel),
        page: String(page),
        limit: QUERY_PAGE_LIMIT,
    });

    const t0 = performance.now();
    if (status) status.textContent = "Executing...";
    fetch("/api/execute-query", { method: "POST", body })
        .then((r) => {
            if (!r.ok) throw r;
            return r.text();
        })
        .then((html) => {
            const tmp = document.createElement("div");
            tmp.innerHTML = html;
            const result = tmp.querySelector(".query-result");
            const elapsed = result.dataset.elapsed || "0";
            const message = result.dataset.message || "";
            if (grid && result) {
                grid.innerHTML = result.innerHTML;
                initDataGridResize(grid, panel);
            }

            const total = parseInt(result.dataset.total) || 0;
            const totalPages = parseInt(result.dataset.totalPages) || 1;
            const curPage = parseInt(result.dataset.page) || 1;
            const rows = grid ? grid.querySelectorAll("tbody tr").length : 0;

            logMessage(panel, "info", "Query executed in " + elapsed + "s");

            if (message) {
                if (status) status.textContent = message + " — " + elapsed + "s";
                logMessage(panel, "success", message);
            } else {
                if (status) status.textContent = rowLabel(total) + " (" + rows + " on page) — " + elapsed + "s";
                logMessage(panel, "success", rowLabel(total) + " returned");
            }

            if (totalPages > 1) {
                pag.classList.remove("hidden");
                pageInfo.textContent = "Page " + curPage + " of " + totalPages;
                pag.dataset.page = curPage;
                pag.dataset.totalPages = totalPages;
            } else {
                pag.classList.add("hidden");
            }

            if (hasSelection && ed) {
                ed.focus(panel, { from: selStart, to: selEnd });
            }
        })
        .catch(async (r) => {
            const elapsed = elapsedSeconds(t0);
            const msg = r.body ? await r.text() : "Request failed";
            grid.innerHTML = '<div class="p-4 text-red-400 text-sm">' + msg + '</div>';
            if (status) status.textContent = "Error — " + elapsed + "s";
            if (pag) pag.classList.add("hidden");

            logMessage(panel, "error", "Query failed after " + elapsed + "s: " + msg);

            const msgTab = panel.querySelector('[data-output-tab="messages"]');
            if (msgTab) switchOutputTab(msgTab);

            if (hasSelection && ed) {
                ed.focus(panel, { from: selStart, to: selEnd });
            }
        });
}

// Runs the current selected/full query wrapped in EXPLAIN (or EXPLAIN
// ANALYZE) and shows the plan in the Explain output tab.
function executeExplain(btn, analyze) {
    const panel = btn.closest("[id^='tab-content']");
    if (!panel) return;
    const form = queryForm(panel);
    const explainPanel = panel.querySelector("#explain-panel > div");
    const status = panel.querySelector(".query-status");
    if (!form) return;

    const params = formConnectionParams(form);
    if (!params) return;

    const ed = window.SqlEditor;
    const doc = ed && ed.view(panel) ? ed.value(panel) : "";
    const sel = ed && ed.view(panel) ? ed.selection(panel) : { from: 0, to: 0 };
    const hasSelection = sel.from !== sel.to;
    const query = hasSelection
        ? doc.substring(sel.from, sel.to)
        : doc;
    if (!query) return;

    const keyword = analyze ? "EXPLAIN ANALYZE" : "EXPLAIN";
    const body = new URLSearchParams({
        sql_query: keyword + " " + query,
        server_id: params.sid.value,
        db_name: params.db.value,
        tab_id: panelTabId(panel),
        page: "1",
        limit: QUERY_PAGE_LIMIT,
    });

    const t0 = performance.now();
    if (status) status.textContent = keyword + "...";
    fetch("/api/execute-query", { method: "POST", body })
        .then((r) => { if (!r.ok) throw r; return r.text(); })
        .then((html) => {
            const tmp = document.createElement("div");
            tmp.innerHTML = html;
            const result = tmp.querySelector(".query-result");
            if (result && explainPanel) {
                explainPanel.innerHTML = result.innerHTML;
            }
            if (status) status.textContent = keyword + " — " + elapsedSeconds(t0) + "s";
            logMessage(panel, "info", keyword + " executed in " + elapsedSeconds(t0) + "s");

            const explainTab = panel.querySelector('[data-output-tab="explain"]');
            if (explainTab) {
                explainTab.classList.remove("hidden");
                switchOutputTab(explainTab);
            }
        })
        .catch(async (r) => {
            const msg = r.body ? await r.text() : "Request failed";
            if (explainPanel) explainPanel.innerHTML = '<div class="p-4 text-red-400 text-sm">' + msg + '</div>';
            if (status) status.textContent = keyword + " — Error";
            logMessage(panel, "error", keyword + " failed after " + elapsedSeconds(t0) + "s: " + msg);
            const explainTab = panel.querySelector('[data-output-tab="explain"]');
            if (explainTab) {
                explainTab.classList.remove("hidden");
                switchOutputTab(explainTab);
            }
        });
}

function goToPage(btn, action) {
    const panel = btn.closest("[id^='tab-content']");
    const pag = panel.querySelector(".pagination-controls");
    let page = parseInt(pag.dataset.page) || 1;
    const totalPages = parseInt(pag.dataset.totalPages) || 1;

    if (action === "prev") page = Math.max(1, page - 1);
    else if (action === "next") page = Math.min(totalPages, page + 1);
    else if (action === "last") page = totalPages;
    else if (typeof action === "number") page = action;

    const execBtn = panel.querySelector("button[onclick='executeQuery(this)']");
    if (execBtn) executeQuery(execBtn, page);
}

function switchScriptPane(tabBtn) {
    const panel = tabBtn.closest("[id^='tab-content']");
    if (!panel) return;
    const name = tabBtn.dataset.scriptTab;

    panel.querySelectorAll(".script-tab").forEach((t) => {
        const active = t.dataset.scriptTab === name;
        t.classList.toggle("text-gray-200", active);
        t.classList.toggle("font-semibold", active);
        t.classList.toggle("text-gray-400", !active);
    });
    panel.querySelectorAll(".script-pane").forEach((p) => {
        p.classList.toggle("hidden", p.dataset.pane !== name);
    });
}

// -----------------------------------------------------------------------------
// Query history side panel
// -----------------------------------------------------------------------------

let historyPanelOpen = false;
let historyDetailOpen = false;
let historySource = null;

function toggleHistoryPanel() {
    historyPanelOpen = !historyPanelOpen;
    const panel = document.getElementById("history-panel");
    const btn = document.getElementById("history-toggle");
    const label = document.getElementById("history-toggle-label");
    if (panel) panel.classList.toggle("hidden", !historyPanelOpen);
    if (label) label.classList.toggle("hidden", !historyPanelOpen);
    if (btn) btn.classList.toggle("text-blue-400", historyPanelOpen);

    if (historyPanelOpen) {
        loadQueryHistory();
        openHistoryStream();
    } else {
        closeHistoryStream();
    }
}

// Subscribes to SSE updates for the current connection so new history
// rows repaint the panel without polling. Reconnects if the connection
// changes while open.
function openHistoryStream() {
    if (!historyPanelOpen) return;
    closeHistoryStream();
    const conn = currentHistoryConnection();
    if (!conn) return;
    const src = new EventSource(
        "/api/query-history/stream?server_id=" + encodeURIComponent(conn.serverID) +
        "&db_name=" + encodeURIComponent(conn.dbName));
    src.onmessage = () => {
        // Only repaint the list, leaving a detail view untouched.
        if (!historyPanelOpen || historyDetailOpen) return;
        loadQueryHistory();
    };
    // EventSource auto-reconnects on error; leave it be.
    historySource = src;
}

function closeHistoryStream() {
    if (historySource) { historySource.close(); historySource = null; }
}

// Resolves the connection to scope the history panel to: the active
// script tab if one exists, otherwise the current tree selection.
function currentHistoryConnection() {
    const activePanel = document.getElementById(TAB_CONTENT_PREFIX + activeTabId);
    if (activePanel) {
        const params = formConnectionParams(queryForm(activePanel));
        if (params && params.sid.value && params.db.value) {
            return { serverID: params.sid.value, dbName: params.db.value };
        }
    }
    if (monitoringConn) return { serverID: String(monitoringConn.serverID), dbName: monitoringConn.dbName };
    return null;
}

// Loads the query history list for the current connection into the
// global history panel.
function loadQueryHistory() {
    const pane = document.getElementById("query-history-pane");
    if (!pane) return;
    historyDetailOpen = false;

    const conn = currentHistoryConnection();
    if (!conn) {
        pane.innerHTML = '<div class="p-3 text-xs text-gray-500 italic">Select a database to view query history.</div>';
        return;
    }

    const params = new URLSearchParams();
    params.set("server_id", conn.serverID);
    params.set("db_name", conn.dbName);
    params.set("tab_id", "");

    pane.innerHTML = '<div class="p-3 text-xs text-gray-500 italic">Loading…</div>';

    fetch("/api/query-history?" + params.toString())
        .then((r) => { if (!r.ok) throw r; return r.text(); })
        .then((html) => {
            pane.innerHTML = html;
            pane.querySelectorAll(".history-item").forEach((item) => {
                item.addEventListener("click", () => useHistoryItem(item));
            });
        })
        .catch(() => {
            pane.innerHTML = '<div class="p-3 text-xs text-red-400">Failed to load query history.</div>';
        });
}

// Loads a history entry's details into the history panel.
function useHistoryItem(item) {
    const id = item.dataset.historyId;
    const serverID = item.dataset.historyServer || "";
    const dbName = item.dataset.historyDb || "";
    if (!id) return;

    const pane = document.getElementById("query-history-pane");
    if (!pane) return;
    historyDetailOpen = true;

    fetch("/api/query-history/" + id + "?server_id=" + serverID + "&db_name=" + dbName)
        .then((r) => { if (!r.ok) throw r; return r.text(); })
        .then((html) => {
            pane.innerHTML = html;
        });
}

function historyBack() {
    loadQueryHistory();
}

function copyToEditor(btn) {
    const code = btn.closest(".border").querySelector("code").innerText;
    const serverID = btn.dataset.serverId;
    const dbName = btn.dataset.dbName;
    openTab("Query", code, serverID, serverNameForID(serverID), dbName);
}

// -----------------------------------------------------------------------------
// Opening, closing and generating script tabs
// -----------------------------------------------------------------------------

// Opens an empty script tab connected to the given database.
function openQueryToolTab(serverID, serverName, dbName) {
    openTab("Query Tool", "", serverID, serverName, dbName);
}

// Generates a unique id for a newly opened script tab. GUIDs keep tab
// ids collision-free across sessions (the same id is persisted to
// workspace_tabs and used as the query_history.tab_id key).
function newTabId() {
    if (window.crypto && crypto.randomUUID) {
        return crypto.randomUUID();
    }
    return "tab-" + (++tabCounter);
}

function openTab(label, query, serverID, serverName, dbName, id) {
    const restored = !!id;
    if (!id) {
        id = newTabId();
    } else {
        const n = parseInt(id.replace("tab-", ""), 10);
        if (!isNaN(n) && n > tabCounter) tabCounter = n;
    }

    // Every new tab is an unsaved script[N].sql*; restored workspace tabs
    // keep their stored label (minus the unsaved marker) and stay clean.
    const metaName = restored
        ? (label || "script.sql").replace(/\*$/, "")
        : "script" + (++scriptCounter) + ".sql";
    const m = /^script(\d+)\.sql$/i.exec(metaName);
    if (m) scriptCounter = Math.max(scriptCounter, parseInt(m[1], 10));
    tabMeta[id] = { name: metaName, path: "", dirty: !restored };

    // Create tab button
    const btn = document.createElement("button");
    btn.className = "tab-btn flex items-center space-x-2 px-4 py-2 font-medium rounded-t text-gray-400 hover:text-gray-200";
    btn.draggable = true;
    btn.dataset.tabId = id;
    btn.innerHTML = '<span>' + displayTabName(id) + '</span>' +
        '<span class="text-xs text-gray-500 hover:text-gray-300" onclick="closeTab(\'' + id + '\', event)">✕</span>';
    btn.addEventListener("click", (e) => {
        if (e.target.textContent === "✕") return;
        switchTab(id);
    });
    const tabBar = document.getElementById(ID_TAB_BAR);
    const historyToggle = document.getElementById("history-toggle");
    // Insert before the history toggle so content tabs stay on the left
    // and the history button always remains right-aligned.
    if (historyToggle) tabBar.insertBefore(btn, historyToggle);
    else tabBar.appendChild(btn);
    updateTabLabel(id);

    // Create content panel and fetch its content from the server
    const panel = document.createElement("div");
    panel.id = TAB_CONTENT_PREFIX + id;
    panel.className = "tab-panel flex-1 flex flex-col overflow-hidden bg-gray-900 text-gray-200";
    panel.style.display = "none";
    document.getElementById("tab-contents").appendChild(panel);

    switchTab(id);

    return fetch("/api/tabs/script-panel")
        .then((r) => r.text())
        .then((html) => {
            panel.innerHTML = html;
            initScriptResizers(panel);

            // Store the connection first so the CodeMirror editor can
            // load DB-aware completions from it.
            if (serverID && dbName) {
                const form = queryForm(panel);
                if (form) {
                    const sid = document.createElement("input");
                    sid.type = "hidden"; sid.name = PARAM_SERVER_ID; sid.value = serverID;
                    form.appendChild(sid);
                    const db = document.createElement("input");
                    db.type = "hidden"; db.name = PARAM_DB_NAME; db.value = dbName;
                    form.appendChild(db);
                }
            }

            // Initialize the CodeMirror SQL editor and pre-fill it.
            if (window.SqlEditor) {
                window.SqlEditor.init(panel);
                if (query) window.SqlEditor.set(panel, query);
            }

            const connBadge = panel.querySelector("#conn-badge");
            if (connBadge) {
                connBadge.textContent = serverID && dbName
                    ? (serverName || "server " + serverID) + " · " + dbName
                    : "";
            }

            // Content auto-save and F5/Mod-Enter execution are handled
            // inside the CodeMirror module (updateListener / keymap),
            // so nothing else needs wiring here.
            scheduleSave();
        });
}

function openSelectScriptTab(tableURL) {
    const t = tableURLParts(tableURL);
    fetch(t.url + "/columns-script")
        .then((r) => r.json())
        .then((data) => {
            openTab("SELECT " + t.tableName, data.query, t.serverID, t.serverName, t.dbName);
        });
}

function openCreateScriptTab(tableURL) {
    const t = tableURLParts(tableURL);
    fetch(t.url + "/create-script")
        .then((r) => { if (!r.ok) throw r; return r.json(); })
        .then((data) => {
            openTab("CREATE " + t.tableName, data.query, t.serverID, t.serverName, t.dbName);
        });
}

function openInsertScriptTab(tableURL) {
    const t = tableURLParts(tableURL);
    fetch(t.url + "/insert-script")
        .then((r) => { if (!r.ok) throw r; return r.json(); })
        .then((data) => {
            openTab("INSERT " + t.tableName, data.query, t.serverID, t.serverName, t.dbName);
        });
}

function openDeleteScriptTab(tableURL) {
    const t = tableURLParts(tableURL);
    fetch(t.url + "/delete-script")
        .then((r) => { if (!r.ok) throw r; return r.json(); })
        .then((data) => {
            openTab("DELETE " + t.tableName, data.query, t.serverID, t.serverName, t.dbName);
        });
}

function closeTab(id, e) {
    if (e) e.stopPropagation();
    const btn = document.querySelector('.tab-btn[data-tab-id="' + id + '"]');
    const panel = document.getElementById(TAB_CONTENT_PREFIX + id);
    if (btn) btn.remove();
    if (panel) panel.remove();
    delete tabMeta[id];

    // Switch to the last remaining tab (Dashboard is always first)
    const remaining = document.querySelectorAll(".tab-btn");
    if (remaining.length > 0) {
        const last = remaining[remaining.length - 1];
        switchTab(last.dataset.tabId);
    }
}

// -----------------------------------------------------------------------------
// Draggable script tabs – reorder by dragging a tab button in #tab-bar.
// Uses native HTML5 drag-and-drop with event delegation so tabs created
// later (openTab) work without extra wiring. The Dashboard tab and the
// history toggle are not reorderable.
// -----------------------------------------------------------------------------
let dragTabId = null;

function initTabDrag() {
    const bar = document.getElementById(ID_TAB_BAR);
    if (!bar) return;

    bar.addEventListener("dragstart", (e) => {
        const btn = e.target.closest(".tab-btn");
        if (!btn || btn.dataset.tabId === TAB_DASHBOARD) {
            e.preventDefault();
            return;
        }
        // Ignore drags that begin on the close (✕) button.
        if (e.target.closest("[onclick^='closeTab']")) {
            e.preventDefault();
            return;
        }
        dragTabId = btn.dataset.tabId;
        btn.classList.add("opacity-50");
        e.dataTransfer.effectAllowed = "move";
        e.dataTransfer.setData("text/plain", btn.dataset.tabId);
    });

    bar.addEventListener("dragover", (e) => {
        e.preventDefault();
        const over = e.target.closest(".tab-btn");
        if (!over || !dragTabId || over.dataset.tabId === TAB_DASHBOARD) return;
        e.dataTransfer.dropEffect = "move";
    });

    bar.addEventListener("drop", (e) => {
        e.preventDefault();
        const over = e.target.closest(".tab-btn");
        if (!over || !dragTabId || over.dataset.tabId === TAB_DASHBOARD) return;
        if (over.dataset.tabId === dragTabId) return;

        const dragBtn = bar.querySelector('.tab-btn[data-tab-id="' + dragTabId + '"]');
        if (!dragBtn) return;

        const rect = over.getBoundingClientRect();
        const after = e.clientX > rect.left + rect.width / 2;

        // Reorder the DOM node: insert before or after the hovered tab.
        if (after && over.nextElementSibling === dragBtn) return;
        if (!after && over === dragBtn) return;
        if (after) {
            bar.insertBefore(dragBtn, over.nextElementSibling);
        } else {
            bar.insertBefore(dragBtn, over);
        }

        dragBtn.classList.remove("opacity-50");
        dragTabId = null;
        scheduleSave();
    });

    bar.addEventListener("dragend", () => {
        const dragBtn = bar.querySelector('.tab-btn[data-tab-id="' + dragTabId + '"]');
        if (dragBtn) dragBtn.classList.remove("opacity-50");
        dragTabId = null;
    });
}

// Scrolling (mouse wheel / trackpad) over the tab bar cycles the
// active tab, like common editors/IDEs. Clamped to the edges; the
// History toggle is not a .tab-btn so it is naturally excluded.
function initTabScroll() {
    const bar = document.getElementById(ID_TAB_BAR);
    if (!bar) return;

    bar.addEventListener("wheel", (e) => {
        if (dragTabId) return;
        if (!e.target.closest(".tab-btn")) return;

        const delta = Math.abs(e.deltaY) >= Math.abs(e.deltaX) ? e.deltaY : e.deltaX;
        if (delta === 0) return;
        e.preventDefault();

        const tabs = Array.from(bar.querySelectorAll(".tab-btn"));
        const active = bar.querySelector(".tab-btn.bg-gray-900");
        const idx = active ? tabs.indexOf(active) : 0;
        if (idx === -1) return;

        const next = tabs[idx + (delta > 0 ? 1 : -1)];
        if (!next) return;
        switchTab(next.dataset.tabId);
        scheduleSave();
    }, { passive: false });
}

// Alt+Shift+Q opens a new empty script tab. When a database (or a node
// under one) is selected in the tree, the tab is bound to that
// connection so running queries works out of the box.
function initTabShortcuts() {
    document.addEventListener("keydown", (e) => {
        if (!e.altKey || !e.shiftKey) return;
        if (!e.key || e.key.toLowerCase() !== "q") return;
        e.preventDefault();

        const selected = document.querySelector(
            '#' + ID_TREE_ROOT + ' button[hx-get][hx-target="#' + selectedTreeId + '"]');
        const conn = connectionFromTreeURL(selected ? selected.getAttribute("hx-get") : null);
        if (conn) {
            openQueryToolTab(conn.serverID, conn.serverName, conn.dbName);
        } else {
            openTab("Query Tool");
        }
    });
}

document.addEventListener("DOMContentLoaded", initTabDrag);
document.addEventListener("DOMContentLoaded", initTabScroll);
document.addEventListener("DOMContentLoaded", initTabShortcuts);
document.addEventListener("DOMContentLoaded", () => {
    const tabBar = document.getElementById(ID_TAB_BAR);
    if (tabBar) tabBar.addEventListener("click", scheduleSave);
});