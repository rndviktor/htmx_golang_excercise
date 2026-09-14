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