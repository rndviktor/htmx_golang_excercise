// -----------------------------------------------------------------------------
// Layout: sidebar + script panel splitter + result-grid column resizing.
// -----------------------------------------------------------------------------

// Shared drag feedback: locks text selection and highlights the handle.
function beginResize(resizer) {
    document.body.classList.add("select-none");
    resizer.classList.add("bg-blue-500");
}

function endResize(resizer) {
    document.body.classList.remove("select-none");
    resizer.classList.remove("bg-blue-500");
}

// Makes the #sidebar panel resizable by dragging #sidebar-resizer.
// Clamped between SIDEBAR_MIN and half the window; the chosen width
// is remembered in localStorage. Double-click resets to the default.
function initSidebarResize() {
    const sidebar = document.getElementById("sidebar");
    const resizer = document.getElementById("sidebar-resizer");
    if (!sidebar || !resizer) return;

    const MIN_WIDTH = 180;
    const DEFAULT_WIDTH = 256; // Tailwind w-64
    let startX = 0, startWidth = 0;

    function setWidth(px) {
        const max = Math.floor(window.innerWidth / 2);
        sidebar.style.width =
            Math.min(max, Math.max(MIN_WIDTH, px)) + "px";
    }

    function onMove(e) {
        setWidth(startWidth + e.clientX - startX);
    }

    function onUp() {
        document.removeEventListener("mousemove", onMove);
        document.removeEventListener("mouseup", onUp);
        endResize(resizer);
        localStorage.setItem(
            "sidebar-width",
            String(parseInt(sidebar.style.width, 10)));
        scheduleSave();
    }

    resizer.addEventListener("mousedown", (e) => {
        e.preventDefault();
        startX = e.clientX;
        startWidth = sidebar.getBoundingClientRect().width;
        beginResize(resizer);
        document.addEventListener("mousemove", onMove);
        document.addEventListener("mouseup", onUp);
    });

    resizer.addEventListener("dblclick", () => {
        localStorage.removeItem("sidebar-width");
        setWidth(DEFAULT_WIDTH);
        scheduleSave();
    });

    const saved = parseInt(localStorage.getItem("sidebar-width"), 10);
    if (!isNaN(saved)) setWidth(saved);
}
document.addEventListener("DOMContentLoaded", initSidebarResize);

// Makes the two splitters inside a script tab panel draggable:
//   .query-resizer-v  -> resizes the Query / Scratch Pad split
//   .query-resizer-h  -> resizes the editors vs. results split
// Sizes are remembered per tab in localStorage and restored when the
// panel is recreated (openTab fetch or workspace restore). Double
// clicking a handle resets that split to its default.
function initScriptResizers(panel) {
    if (!panel) return;
    const vsplit = panel.querySelector(".query-vsplit");
    const leftPane = panel.querySelector(".q-editor");
    const resizerV = panel.querySelector(".query-resizer-v");
    const resizerH = panel.querySelector(".query-resizer-h");

    const vKey = "script-split-v-" + (panel.id || "tab");
    const hKey = "script-split-h-" + (panel.id || "tab");

    try {
        const savedV = parseInt(localStorage.getItem(vKey), 10);
        if (savedV && leftPane) leftPane.style.width = savedV + "px";
        const savedH = parseInt(localStorage.getItem(hKey), 10);
        if (savedH && vsplit) vsplit.style.height = savedH + "px";
    } catch (e) {}

    if (resizerV && leftPane && vsplit) {
        resizerV.addEventListener("mousedown", (e) => {
            e.preventDefault();
            const startX = e.clientX;
            const startW = leftPane.getBoundingClientRect().width;
            const onMove = (move) => {
                let w = startW + move.clientX - startX;
                w = Math.min(w, vsplit.getBoundingClientRect().width - 160);
                leftPane.style.width = Math.max(w, 160) + "px";
            };
            const onUp = () => {
                document.removeEventListener("mousemove", onMove);
                document.removeEventListener("mouseup", onUp);
                endResize(resizerV);
                try {
                    localStorage.setItem(vKey, String(leftPane.getBoundingClientRect().width));
                } catch (err) {}
                scheduleSave();
            };
            beginResize(resizerV);
            document.addEventListener("mousemove", onMove);
            document.addEventListener("mouseup", onUp);
        });
        resizerV.addEventListener("dblclick", () => {
            leftPane.style.width = "50%";
            try { localStorage.removeItem(vKey); } catch (err) {}
        });
    }

    if (resizerH && vsplit) {
        resizerH.addEventListener("mousedown", (e) => {
            e.preventDefault();
            const startY = e.clientY;
            const startH = vsplit.getBoundingClientRect().height;
            const onMove = (move) => {
                let h = startH + move.clientY - startY;
                h = Math.min(h, panel.getBoundingClientRect().height - 140);
                vsplit.style.height = Math.max(h, 120) + "px";
            };
            const onUp = () => {
                document.removeEventListener("mousemove", onMove);
                document.removeEventListener("mouseup", onUp);
                endResize(resizerH);
                try {
                    localStorage.setItem(hKey, String(vsplit.getBoundingClientRect().height));
                } catch (err) {}
                scheduleSave();
            };
            beginResize(resizerH);
            document.addEventListener("mousemove", onMove);
            document.addEventListener("mouseup", onUp);
        });
        resizerH.addEventListener("dblclick", () => {
            vsplit.style.height = "300px";
            try { localStorage.removeItem(hKey); } catch (err) {}
        });
    }
}

// Makes the result table inside #data-grid resizable:
//   - table-layout: fixed with a per-column width in px
//   - column widths sum to the table width, so no stretchy leftover
//     is redistributed over the columns (this is what removes the
//     jump at drag start); dragging a column wider than the container
//     makes the table overflow and #data-grid's overflow-auto shows a
//     horizontal scrollbar
//   - dragging the thin handle on a header resizes that column;
//     double-clicking it resets to the default layout
// The leading row-number column defaults to a minimal width; the rest
// split the remaining space equally. Column widths are remembered per
// tab in localStorage and restored on the next execution.
function initDataGridResize(grid, panel) {
    const table = grid && grid.querySelector("table");
    if (!table) return;

    table.style.tableLayout = "fixed";
    table.style.width = "auto";

    const headers = Array.from(table.querySelectorAll("thead th"));
    if (headers.length === 0) return;
    const rows = Array.from(table.querySelectorAll("tbody tr"));

    const MIN_WIDTH = 40;
    const ROW_NUM_WIDTH = 44;
    const key = "script-cols-" + (panel && panel.id ? panel.id : "tab");

    let widths = null;
    try {
        const saved = JSON.parse(localStorage.getItem(key) || "null");
        if (Array.isArray(saved) && saved.length === headers.length) widths = saved;
    } catch (e) {}

    // Default layout: the leading row-number column stays minimal,
    // the remaining columns split the leftover space equally.
    const makeEqual = () => {
        const first = ROW_NUM_WIDTH;
        const rest = headers.length > 1
            ? Math.max(MIN_WIDTH, Math.floor(((grid.clientWidth || 0) - first) / (headers.length - 1)))
            : MIN_WIDTH;
        return headers.map((_, i) => i === 0 ? first : rest);
    };
    widths = widths || makeEqual();

    headers.forEach((th, i) => {
        th.style.width = widths[i] + "px";
        th.classList.add("relative", "select-none", "whitespace-nowrap", "overflow-hidden", "text-ellipsis");
        rows.forEach((tr) => {
            const cell = tr.children[i];
            if (cell) cell.classList.add("whitespace-nowrap", "overflow-hidden", "text-ellipsis");
        });

        if (th._resizer) return;
        const handle = document.createElement("div");
        handle.className = "absolute top-0 right-0 h-full w-1.5 cursor-col-resize z-20";
        handle.title = "Drag to resize column";
        handle.addEventListener("mousedown", (e) => {
            e.preventDefault();
            const startX = e.clientX;
            const startW = parseFloat(th.style.width) || th.getBoundingClientRect().width;
            const onMove = (move) => {
                th.style.width = Math.max(MIN_WIDTH, startW + move.clientX - startX) + "px";
            };
            const onUp = () => {
                document.removeEventListener("mousemove", onMove);
                document.removeEventListener("mouseup", onUp);
                endResize(handle);
                commit();
            };
            beginResize(handle);
            document.addEventListener("mousemove", onMove);
            document.addEventListener("mouseup", onUp);
        });
        handle.addEventListener("dblclick", () => {
            widths = makeEqual();
            widths.forEach((w, j) => { headers[j].style.width = w + "px"; });
            commit();
        });
        th.appendChild(handle);
        th._resizer = handle;
    });

    function commit() {
        const arr = headers.map((th) => parseInt(th.style.width, 10) || 0);
        try { localStorage.setItem(key, JSON.stringify(arr)); } catch (e) {}
    }
}