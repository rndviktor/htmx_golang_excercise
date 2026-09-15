// -----------------------------------------------------------------------------
// Find widget (Ctrl+F): client-side search over the currently active script
// tab's CodeMirror document. The widget markup lives in script_tab_panel.html;
// SqlEditor.search/searchStep/clearSearch do the actual matching.
// -----------------------------------------------------------------------------

const FIND_WIDGET = "[data-find-widget]";

// Returns the panel of the currently active script tab, if any.
function activeScriptPanel() {
    if (activeTabId === TAB_DASHBOARD || !window.SqlEditor) return null;
    const panel = document.getElementById(TAB_CONTENT_PREFIX + activeTabId);
    return panel && panel.querySelector("[data-sql-editor]") ? panel : null;
}

function findWidget(panel) {
    return panel ? panel.querySelector(FIND_WIDGET) : null;
}

function findOptions(widget) {
    return {
        caseSensitive: widget.querySelector(".find-match-case").checked,
        regex: widget.querySelector(".find-regex").checked,
    };
}

function searchQuery(widget) {
    const input = widget.querySelector('input[name="search"]');
    return input ? input.value : "";
}

// Highlights all matches of the current widget query.
function runFind(panel, widget) {
    if (window.SqlEditor) window.SqlEditor.search(panel, searchQuery(widget), findOptions(widget));
}

// Moves the selection to the previous/next match (dir: -1 or +1).
function findStep(panel, widget, dir) {
    if (window.SqlEditor) {
        window.SqlEditor.searchStep(panel, searchQuery(widget), findOptions(widget), dir);
    }
}

// Hides the widget and removes all highlights.
function closeFind(panel, widget) {
    if (widget) widget.classList.add("hidden");
    if (window.SqlEditor) {
        window.SqlEditor.clearSearch(panel);
        const view = window.SqlEditor.view(panel);
        if (view) view.focus();
    }
}

// Wires one widget instance (script panels are created dynamically, so this
// runs lazily on first Ctrl+F for a tab).
function initFindWidget(widget) {
    const panel = widget.closest(".tab-panel");
    const input = widget.querySelector('input[name="search"]');

    input.addEventListener("input", () => runFind(panel, widget));
    input.addEventListener("keydown", (e) => {
        if (e.key === "Enter") {
            e.preventDefault();
            findStep(panel, widget, e.shiftKey ? -1 : 1);
        } else if (e.key === "Escape") {
            e.preventDefault();
            closeFind(panel, widget);
        }
    });

    widget.querySelectorAll(".btn-toggle").forEach((btn) => {
        btn.addEventListener("click", () => {
            const isCase = btn.dataset.findToggle === "match-case";
            const checkbox = widget.querySelector(isCase ? ".find-match-case" : ".find-regex");
            checkbox.checked = !checkbox.checked;
            btn.classList.toggle("bg-gray-600", checkbox.checked);
            btn.classList.toggle("text-blue-400", checkbox.checked);
            btn.classList.toggle("ring-1", checkbox.checked);
            btn.classList.toggle("ring-blue-400", checkbox.checked);
            runFind(panel, widget);
        });
    });

    widget.querySelector(".find-match-case").addEventListener("change", () => runFind(panel, widget));
    widget.querySelector(".find-regex").addEventListener("change", () => runFind(panel, widget));

    widget.querySelector(".btn-find-prev").addEventListener("click", () => findStep(panel, widget, -1));
    widget.querySelector(".btn-find-next").addEventListener("click", () => findStep(panel, widget, 1));
    widget.querySelector(".btn-close").addEventListener("click", () => closeFind(panel, widget));

    widget.dataset.findReady = "1";
}

// Shows the widget for `panel` (focusing its input); re-focuses and selects
// the query when it is already visible.
function toggleFindWidget(panel) {
    if (!panel) return;
    const widget = findWidget(panel);
    if (!widget) return;
    if (!widget.dataset.findReady) initFindWidget(widget);

    const input = widget.querySelector('input[name="search"]');
    if (widget.classList.contains("hidden")) {
        widget.classList.remove("hidden");
        input.focus();
        input.select();
        runFind(panel, widget);
    } else {
        input.focus();
        input.select();
    }
}

// Ctrl+F / Cmd+F opens the find widget for the active script tab. When the
// active tab is not a script tab, the native browser find is left alone.
document.addEventListener("keydown", (e) => {
    if (!(e.ctrlKey || e.metaKey)) return;
    if (!e.key || e.key.toLowerCase() !== "f") return;
    const panel = activeScriptPanel();
    if (!panel) return;
    e.preventDefault();
    toggleFindWidget(panel);
});