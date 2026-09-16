// -----------------------------------------------------------------------------
// Object explorer tree: lazy expansion, selection highlighting and restoring
// the expanded/saved state after a refresh.
// -----------------------------------------------------------------------------

// Collapses an expanded tree node. Returns true when the node was
// open (caller should cancel the pending htmx request), false when
// it should be fetched/expanded.
function toggleTreeChildren(containerId) {
    const el = document.getElementById(containerId);
    if (el && el.childElementCount > 0) {
        el.innerHTML = "";
        return true;
    }
    return false;
}

// Highlights the currently selected tree node (the one whose children
// container matches selectedTreeId).
function highlightTreeSelection() {
    document.querySelectorAll("#" + ID_TREE_ROOT + " button[hx-get]").forEach((b) => {
        const active = b.getAttribute("hx-target") === "#" + selectedTreeId;
        b.classList.toggle("text-blue-400", active);
        b.classList.toggle("font-semibold", active);
    });
}

// Expands one lazily-loaded tree node by fetching its children exactly
// like htmx would (the button and its container are siblings).
// Resolves with true when the node was (or already is) expanded,
// false when its container does not exist yet (parent not loaded).
function expandTreeContainer(id) {
    const container = document.getElementById(id);
    if (container && container.childElementCount > 0) return Promise.resolve(true);
    if (!container) return Promise.resolve(false);
    const btn = container.previousElementSibling;
    if (!btn || !btn.hasAttribute("hx-get")) return Promise.resolve(false);
    const url = btn.getAttribute("hx-get");
    return fetch(url)
        .then((r) => { if (!r.ok) throw r; return r.text(); })
        .then((html) => {
            container.innerHTML = html;
            if (window.htmx && htmx.process) htmx.process(container);
            return true;
        })
        .catch(() => false);
}

// Switches the status dot of a server tree node (el is the server's <li>).
// state is "on" (green) or "gray" (deliberately disconnected). The gray dot
// uses an inline background color because the precompiled tailwind.css does
// not carry a gray filler class for this dot. The state is also mirrored on
// the <li> via data-tree-state so the context menu does not depend on the
// dot's visual color.
function setServerDot(el, state) {
    el.setAttribute("data-tree-state", state);
    const dot = el.querySelector("button span.rounded-full");
    if (!dot) return;
    dot.classList.remove("bg-green-500", "bg-red-500");
    if (state === "on") {
        dot.style.backgroundColor = "";
        dot.classList.add("bg-green-500");
    } else {
        dot.style.backgroundColor = "#94a3b8";
    }
}

// Refreshes one lazily-loaded tree node in place: re-fetches its children and
// then re-expands every descendant that was expanded before the refresh, so
// the previously visible subtree stays open and is re-populated. el is the
// node's <li> element carrying the expand button. For a server node the
// refresh goes through /reconnect, rendering the children on success and the
// "not available" hint otherwise. Resolves with true when the node was
// refreshed, false otherwise.
function refreshTreeNode(el) {
    const btn = el.querySelector("button[hx-get]");
    if (!btn) return Promise.resolve(false);
    const target = btn.getAttribute("hx-target");
    const container = target && document.querySelector(target);
    if (!container) return Promise.resolve(false);

    // Remember which descendant containers are currently expanded so they can
    // be re-fetched after the node's children are replaced in place.
    const expanded = [];
    container.querySelectorAll("[id]").forEach((c) => {
        if (c.childElementCount > 0) expanded.push(c.id);
    });

    // Server nodes refresh by reconnecting: /reconnect returns the folders on
    // success and the "not available" hint when the connection cannot be made.
    const kind = el.getAttribute("data-tree-menu") || "";
    let url = btn.getAttribute("hx-get");
    if (kind === "server") url = url.replace(/\/children$/, "/reconnect");

    return fetch(url)
        .then((r) => { if (!r.ok) throw r; return r.text(); })
        .then((html) => {
            container.innerHTML = html;
            if (window.htmx && htmx.process) htmx.process(container);

            // A reconnected server turns its dot green again; a failed
            // reconnect leaves the dot as it was (gray when deliberately
            // disconnected, red when unavailable).
            if (kind === "server" && container.querySelector("ul button[hx-get]")) {
                setServerDot(el, "on");
            }

            // Re-expand previously expanded descendants one range at a time,
            // waiting for a parent before its expanded children can load.
            const remaining = expanded.slice();
            function step() {
                const pending = remaining.slice();
                remaining.length = 0;
                let progressed = false;
                let chain = Promise.resolve();
                pending.forEach((id) => {
                    chain = chain.then(() =>
                        expandTreeContainer(id).then((done) => {
                            if (done) progressed = true;
                            else remaining.push(id);
                        }));
                });
                chain.then(() => {
                    if (progressed && remaining.length > 0) step();
                });
            }
            step();
        })
        .catch(() => false);
}

// Re-expands the saved tree state after a page refresh. Parent nodes
// are fetched before children by retrying in rounds until no pending
// node can be expanded, then the saved selection is highlighted.
function applyTreeRestore() {
    if (treeRestoreApplied || !pendingTreeState) return;
    if (!document.getElementById(ID_SERVERS_GROUP)) return;
    treeRestoreApplied = true;
    const state = pendingTreeState;
    pendingTreeState = null;

    const remaining = (state.expanded_tree || []).slice();
    if (state.selected_tree && !remaining.includes(state.selected_tree)) {
        remaining.push(state.selected_tree);
    }

    function step() {
        const pending = remaining.slice();
        remaining.length = 0;
        let progressed = false;
        let chain = Promise.resolve();
        pending.forEach((id) => {
            chain = chain.then(() =>
                expandTreeContainer(id).then((done) => {
                    if (done) progressed = true;
                    else remaining.push(id);
                }));
        });
        chain.then(() => {
            if (progressed && remaining.length > 0) {
                step();
            } else {
                if (state.selected_tree) selectedTreeId = state.selected_tree;
                highlightTreeSelection();
                // Selecting a database (or anything below it) via the
                // restored workspace should show monitoring, just like
                // a real click.
                const btn = document.querySelector(
                    '#' + ID_TREE_ROOT + ' button[hx-get][hx-target="#' + selectedTreeId + '"]');
                if (btn) updateDashboardForTreeSelection(btn);
            }
        });
    }
    step();
}

// Waits (polling) until the root tree node exists, i.e. the tree has
// been loaded, then invokes cb.
function waitForTreeRoot(cb, tries) {
    const el = document.getElementById(ID_SERVERS_GROUP);
    if (el) { cb(); return; }
    if (tries <= 0) return;
    setTimeout(() => waitForTreeRoot(cb, tries - 1), 100);
}

// Remembers the clicked tree node and persists the selection.
document.addEventListener("click", (e) => {
    const btn = e.target.closest("#" + ID_TREE_ROOT + " button[hx-get]");
    if (!btn) return;
    const target = btn.getAttribute("hx-target");
    if (!target) return;
    selectedTreeId = target.replace(/^#/, "");
    highlightTreeSelection();
    scheduleSave();
    updateDashboardForTreeSelection(btn);
});