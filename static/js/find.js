// -----------------------------------------------------------------------------
// Find (Ctrl+F) and Find & Replace (Ctrl+R) widgets: client-side search and
// replacement over the currently active script tab's CodeMirror document.
// The widget markup lives in script_tab_panel.html;
// SqlEditor.search/searchStep/replaceNext/replaceAll/clearSearch do the work.
// -----------------------------------------------------------------------------

const FIND_WIDGET = "[data-find-widget]";
const FIND_REPLACE_WIDGET = "[data-findreplace-widget]";
const BOTH_WIDGETS = FIND_WIDGET + ", " + FIND_REPLACE_WIDGET;

// Returns the panel of the currently active script tab, if any.
function activeScriptPanel() {
    if (activeTabId === TAB_DASHBOARD || !window.SqlEditor) return null;
    const panel = document.getElementById(TAB_CONTENT_PREFIX + activeTabId);
    return panel && panel.querySelector("[data-sql-editor]") ? panel : null;
}

function findWidget(panel) {
    return panel ? panel.querySelector(FIND_WIDGET) : null;
}

function findReplaceWidget(panel) {
    return panel ? panel.querySelector(FIND_REPLACE_WIDGET) : null;
}

function panelOf(widget) {
    return widget.closest(".tab-panel");
}

// Both widgets share the same option-checkbox classes, so the search options
// resolve for either widget.
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

function focusSearchInput(widget) {
    const input = widget.querySelector('input[name="search"]');
    input.focus();
    input.select();
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

// Hides the widget (if given) and removes all highlights.
function closeWidget(panel, widget) {
    if (widget) widget.classList.add("hidden");
    if (window.SqlEditor) {
        window.SqlEditor.clearSearch(panel);
        const view = window.SqlEditor.view(panel);
        if (view) view.focus();
    }
}

// Wires the match-case / regex toggles shared by both widgets.
function initToggleButtons(widget) {
    const panel = panelOf(widget);
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
}

// Wires the pieces both widgets share (search input, toggles, nav + close
// buttons). onEnter handles the search-input Enter (and Shift+Enter) key.
function wireFindControls(panel, widget, onEnter) {
    const input = widget.querySelector('input[name="search"]');
    input.addEventListener("input", () => runFind(panel, widget));
    input.addEventListener("keydown", (e) => {
        if (e.key === "Enter") {
            e.preventDefault();
            onEnter(e);
        } else if (e.key === "Escape") {
            e.preventDefault();
            closeWidget(panel, widget);
        }
    });

    initToggleButtons(widget);

    widget.querySelector(".btn-find-prev").addEventListener("click", () => findStep(panel, widget, -1));
    widget.querySelector(".btn-find-next").addEventListener("click", () => findStep(panel, widget, 1));
    widget.querySelector(".btn-close").addEventListener("click", () => closeWidget(panel, widget));
}

// --- Initializers (idempotent; run lazily on first open of a tab) ----------

function initFindWidget(widget) {
    if (widget.dataset.findReady) return;
    const panel = panelOf(widget);
    wireFindControls(panel, widget, (e) => findStep(panel, widget, e.shiftKey ? -1 : 1));
    widget.dataset.findReady = "1";
}

function initFindReplaceWidget(widget) {
    if (widget.dataset.findReplaceReady) return;
    const panel = panelOf(widget);
    const replaceInput = widget.querySelector('input[name="replace"]');

    function replaceWith() {
        if (window.SqlEditor && searchQuery(widget)) {
            window.SqlEditor.replaceNext(panel, searchQuery(widget),
                replaceInput.value, findOptions(widget));
        }
    }

    wireFindControls(panel, widget, (e) => findStep(panel, widget, e.shiftKey ? -1 : 1));

    replaceInput.addEventListener("keydown", (e) => {
        if (e.key === "Enter") {
            e.preventDefault();
            replaceWith();
        } else if (e.key === "Escape") {
            e.preventDefault();
            closeWidget(panel, widget);
        }
    });

    widget.querySelector(".btn-replace").addEventListener("click", replaceWith);
    widget.querySelector(".btn-replace-all").addEventListener("click", () => {
        if (window.SqlEditor && searchQuery(widget)) {
            window.SqlEditor.replaceAll(panel, searchQuery(widget),
                replaceInput.value, findOptions(widget));
        }
    });

    widget.dataset.findReplaceReady = "1";
}

// --- Open/toggle ------------------------------------------------------------

// Shows `widget` for its panel, hiding the sibling widget; re-focuses and
// selects the query when it is already visible.
function showWidget(panel, widget, init) {
    if (!widget) return;
    panel.querySelectorAll(BOTH_WIDGETS).forEach((w) => {
        if (w !== widget) w.classList.add("hidden");
    });
    init(widget);
    if (widget.classList.contains("hidden")) {
        widget.classList.remove("hidden");
        focusSearchInput(widget);
        runFind(panel, widget);
    } else {
        focusSearchInput(widget);
    }
}

function toggleFindWidget(panel) {
    showWidget(panel, findWidget(panel), initFindWidget);
}

function toggleFindReplaceWidget(panel) {
    showWidget(panel, findReplaceWidget(panel), initFindReplaceWidget);
}

// Ctrl+F / Cmd+F (find) and Ctrl+R / Cmd+R (find & replace, normally a
// browser reload) open the matching widget for the active script tab. When
// the active tab is not a script tab, the native browser behavior is left
// alone.
document.addEventListener("keydown", (e) => {
    if (!(e.ctrlKey || e.metaKey)) return;
    if (!e.key) return;
    const panel = activeScriptPanel();
    if (!panel) return;
    const key = e.key.toLowerCase();
    if (key === "f") {
        e.preventDefault();
        toggleFindWidget(panel);
    } else if (key === "r") {
        e.preventDefault();
        toggleFindReplaceWidget(panel);
    }
});