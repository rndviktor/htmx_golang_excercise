// -----------------------------------------------------------------------------
// Database Monitoring Dashboard – KPI cards, live charts and the SSE stream.
// Polling only runs while the Dashboard tab is active (see
// updateMonitoringForActiveTab).
// -----------------------------------------------------------------------------
let monitoringTimer = null;
let monitoringBaseURL = null;
let monitoringInitialized = false;
let monitoringConn = null;
let monitoringSource = null;
let monitoringInterval = 5000;
let monitoringState = {
    blksHit: 0, blksRead: 0,
    xactCommit: 0, xactRollback: 0,
    inserts: 0, updates: 0, deletes: 0,
};
const chartSeries = {
    cacheHit: [], replication: [], tps: [], ins: [], upd: [], del: [], time: [],
};
let charts = {};

function formatBytes(bytes) {
    if (bytes === null || bytes === undefined || bytes < 0) return "–";
    return formatUnits(bytes, { bytes: ["B", "KB", "MB", "GB", "TB"] });
}

function formatUnits(val, units) {
    let i = 0, n = val;
    while (n >= 1024 && i < units.bytes.length - 1) { n /= 1024; i++; }
    return n.toFixed(i === 0 ? 0 : 1) + " " + units.bytes[i];
}

function makeChart(canvasId, type, labels, datasets) {
    const canvas = document.getElementById(canvasId);
    if (!canvas) return null;
    if (charts[canvasId]) charts[canvasId].destroy();
    const ctx = canvas.getContext("2d");
    if (!ctx) return null;
    charts[canvasId] = new Chart(ctx, {
        type: type,
        data: { labels: labels, datasets: datasets },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            animation: false,
            plugins: { legend: { labels: { color: "#cbd5e1", boxWidth: 12, font: { size: 10 } } } },
            scales: {
                x: { ticks: { color: "#64748b", maxTicksLimit: 10 }, grid: { color: "#334155" } },
                y: { ticks: { color: "#64748b" }, grid: { color: "#1e293b", beginAtZero: true } },
            },
        },
    });
    return charts[canvasId];
}

function renderMonitoringCharts() {
    const labels = chartSeries.time;
    const datasets = [];

    // Cache hit ratio (line, %) — computed from cumulative hits/reads deltas.
    makeChart("chart-cache-hit", "line", labels, [{
        label: "Cache Hit %",
        data: chartSeries.cacheHit,
        borderColor: "#3b82f6", backgroundColor: "rgba(59,130,246,0.1)",
        fill: true, tension: 0.3, pointRadius: 0,
    }]);

    // Replication lag (line, bytes).
    makeChart("chart-replication", "line", labels, [{
        label: "Lag (bytes)",
        data: chartSeries.replication,
        borderColor: "#a855f7", backgroundColor: "rgba(168,85,247,0.1)",
        fill: true, tension: 0.3, pointRadius: 0,
    }]);

    // Transaction throughput (line, TPS). commits+rollbacks.
    makeChart("chart-txn", "line", labels, [{
        label: "Commits/s",
        data: chartSeries.tps,
        borderColor: "#22c55e", backgroundColor: "rgba(34,197,94,0.1)",
        fill: true, tension: 0.3, pointRadius: 0,
    }]);

    // Row operations (line). inserts / updates / deletes per second.
    makeChart("chart-rowops", "line", labels, [
        { label: "Inserts/s", data: chartSeries.ins, borderColor: "#3b82f6", tension: 0.3, pointRadius: 0 },
        { label: "Updates/s", data: chartSeries.upd, borderColor: "#eab308", tension: 0.3, pointRadius: 0 },
        { label: "Deletes/s", data: chartSeries.del, borderColor: "#ef4444", tension: 0.3, pointRadius: 0 },
    ]);

}

function updateKPIs(data) {
    document.getElementById("kpi-active-conn").textContent = data.activeConnections;
    document.getElementById("kpi-max-conn").querySelector(".kpi-max-val").textContent =
        (data.maxConnections || 0).toString();
    document.getElementById("kpi-db-size").textContent = formatBytes(data.dbSizeBytes);
    document.getElementById("kpi-active-tx").textContent = data.activeTx;
    document.getElementById("kpi-idle-tx").textContent = data.idleTx;
    document.getElementById("kpi-idle").textContent = data.idle;
    document.getElementById("kpi-blocked").textContent = data.blockedQueries;

    const lagEl = document.getElementById("kpi-repl-lag");
    if (data.hasReplication) {
        lagEl.textContent = formatUnits(data.replicationLagBytes, { bytes: ["B", "KB", "MB", "GB", "TB"] });
        document.getElementById("kpi-repl-unit").textContent = "";
    } else {
        lagEl.textContent = "–";
        document.getElementById("kpi-repl-unit").textContent = "no replica";
    }
}

function refreshMonitoring() {
    if (!monitoringBaseURL) return;
    // Same-origin fetch with credentials so the signed auth cookie is
    // sent (matching htmx's default). Without it RequireAuth redirects
    // to /login and fetch reports "Failed to fetch".
    fetch(monitoringBaseURL, { credentials: "same-origin", headers: { "HX-Request": "true" } })
        .then(async (r) => {
            if (!r.ok) {
                let msg = "HTTP " + r.status;
                try { const t = await r.text(); if (t) msg += ": " + t; } catch (_) {}
                throw new Error(msg);
            }
            return r.json();
        })
        .then((data) => {
            updateKPIs(data);

            // The first sample only establishes the baseline for the
            // cumulative counters (pg_stat_* hold totals since start),
            // so no time-series point is produced until the 2nd poll.
            if (!monitoringInitialized) {
                monitoringInitialized = true;
                monitoringState.blksHit = data.blksHit;
                monitoringState.blksRead = data.blksRead;
                monitoringState.xactCommit = data.xactCommit;
                monitoringState.xactRollback = data.xactRollback;
                monitoringState.inserts = data.inserts;
                monitoringState.updates = data.updates;
                monitoringState.deletes = data.deletes;
                renderMonitoringCharts();
                if (typeof refreshSessions === 'function') refreshSessions();
                if (typeof refreshLocks === 'function') refreshLocks();
                if (typeof refreshPrepared === 'function') refreshPrepared();
                return;
            }

            // Cache hit ratio since last sample (%). Use the buffer delta.
            const blksHitDelta = data.blksHit - monitoringState.blksHit;
            const blksReadDelta = data.blksRead - monitoringState.blksRead;
            const total = blksHitDelta + blksReadDelta;
            monitoringState.blksHit = data.blksHit;
            monitoringState.blksRead = data.blksRead;
            const cacheHit = total > 0 ? (blksHitDelta / total) * 100 : 100;
            chartSeries.cacheHit.push(cacheHit);

            // TPS since last sample.
            const commitDelta = data.xactCommit - monitoringState.xactCommit;
            const rollbackDelta = data.xactRollback - monitoringState.xactRollback;
            monitoringState.xactCommit = data.xactCommit;
            monitoringState.xactRollback = data.xactRollback;
            const tps = commitDelta + rollbackDelta;
            chartSeries.tps.push(tps);

            // Row op rates since last sample.
            chartSeries.ins.push(data.inserts - monitoringState.inserts);
            chartSeries.upd.push(data.updates - monitoringState.updates);
            chartSeries.del.push(data.deletes - monitoringState.deletes);
            monitoringState.inserts = data.inserts;
            monitoringState.updates = data.updates;
            monitoringState.deletes = data.deletes;

            // Replication lag.
            chartSeries.replication.push(
                data.hasReplication ? data.replicationLagBytes : 0);

            // Sessions breakdown (latest snapshot).
            if (typeof refreshSessions === 'function') refreshSessions();
            if (typeof refreshLocks === 'function') refreshLocks();
            if (typeof refreshPrepared === 'function') refreshPrepared();

            // Keep a rolling window of ~30 points.
            chartSeries.time.push(new Date().toLocaleTimeString());
            ["cacheHit", "tps", "ins", "upd", "del", "replication", "time"].forEach((k) => {
                if (chartSeries[k].length > 30) chartSeries[k].shift();
            });

            renderMonitoringCharts();
        })
        .catch((err) => {
            setMonitoringError(err && err.message ? err.message : String(err));
        });
}

function setMonitoringError(msg) {
    const el = document.getElementById("monitoring-error");
    if (!el) return;
    el.textContent = "Monitoring error: " + msg;
    el.classList.remove("hidden");
    clearTimeout(setMonitoringError._t);
    setMonitoringError._t = setTimeout(() => el.classList.add("hidden"), 8000);
}

// Switches the Dashboard tab button between "Dashboard" and "Monitor"
// (with an activity pulse icon) depending on whether monitoring is live.
function setDashboardTabLabel(monitoring) {
    const label = document.getElementById("tab-dashboard-label");
    const icon = document.getElementById("tab-dashboard-icon");
    if (monitoring) {
        // Monitoring mode: single icon only, hide text and spacing.
        if (icon) icon.textContent = "🟢";
        if (label) label.style.display = "none";
    } else {
        if (icon) icon.textContent = "📊";
        if (label) { label.style.display = ""; label.textContent = "Dashboard"; }
    }
}

// Opens the SSE KPI stream for the current connection so KPI cards update
// instantly between the 5s chart polls. Only runs while the Dashboard
// tab is active.
function openMonitoringKPIStream() {
    closeMonitoringKPIStream();
    if (!monitoringConn || activeTabId !== TAB_DASHBOARD) return;
    const src = new EventSource(
        "/api/servers/" + encodeURIComponent(monitoringConn.serverID) +
        "/databases/" + encodeURIComponent(monitoringConn.dbName) + "/monitoring/stream");
    src.onmessage = (e) => {
        let data;
        try { data = JSON.parse(e.data); } catch (_) { return; }
        updateKPIs(data);
    };
    monitoringSource = src;
}

function closeMonitoringKPIStream() {
    if (monitoringSource) { monitoringSource.close(); monitoringSource = null; }
}

function startMonitoring(serverID, dbName) {
    monitoringBaseURL = "/api/servers/" + serverID + "/databases/" + dbName + "/monitoring";
    monitoringConn = { serverID: serverID, dbName: dbName };
    setDashboardTabLabel(true);

    // Reset cumulative counters so the first sample establishes a clean
    // baseline and no historical spike appears.
    monitoringInitialized = false;
    monitoringState = { blksHit: 0, blksRead: 0, xactCommit: 0, xactRollback: 0, inserts: 0, updates: 0, deletes: 0 };
    Object.keys(chartSeries).forEach((k) => { chartSeries[k] = []; });

    const defaultView = document.getElementById("dashboard-default");
    const monView = document.getElementById("dashboard-monitoring");
    if (defaultView) defaultView.classList.add("hidden");
    if (monView) monView.classList.remove("hidden");
    const label = document.getElementById("monitoring-conn-label");
    if (label) label.textContent = dbName + " (server " + serverID + ")";

    // Initial render, then poll charts while the Dashboard tab is active.
    renderMonitoringCharts();
    if (activeTabId !== TAB_DASHBOARD) return;
    pollMonitoring();
    openMonitoringKPIStream();
}

function pollMonitoring() {
    if (!monitoringBaseURL) return;
    refreshMonitoring();
    if (monitoringTimer) clearInterval(monitoringTimer);
    monitoringTimer = setInterval(refreshMonitoring, monitoringInterval);
}

// Sets the chart polling interval (ms) and restarts the timer live.
function setMonitoringInterval(val) {
    const ms = parseInt(val, 10);
    if (isNaN(ms) || ms < 1000) return;
    monitoringInterval = ms;
    if (monitoringTimer && monitoringBaseURL) pollMonitoring();
}

function stopMonitoring() {
    if (monitoringTimer) { clearInterval(monitoringTimer); monitoringTimer = null; }
    monitoringBaseURL = null;
    monitoringConn = null;
    closeMonitoringKPIStream();
    setDashboardTabLabel(false);
    const defaultView = document.getElementById("dashboard-default");
    const monView = document.getElementById("dashboard-monitoring");
    if (defaultView) defaultView.classList.remove("hidden");
    if (monView) monView.classList.add("hidden");
}

// Decides, from the clicked tree node's URL, whether a database (or
// anything below it) is selected and the monitoring dashboard should
// be shown. Any node scoped under /databases/{db} shares that database
// connection, so schema, table and their category folders all qualify.
function updateDashboardForTreeSelection(btn) {
    const url = btn ? btn.getAttribute("hx-get") : null;
    const conn = connectionFromTreeURL(url);
    if (conn) {
        startMonitoring(conn.serverID, conn.dbName);
    } else {
        stopMonitoring();
    }
}