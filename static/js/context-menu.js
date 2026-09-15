// -----------------------------------------------------------------------------
// Right-click context menu for tree nodes carrying [data-tree-menu].
// Purely visual for now: items are placeholders without server actions.
// -----------------------------------------------------------------------------
function initContextMenu() {
    const menu = document.getElementById("ctx-menu");
    if (!menu) return;

    let currentTableURL = null;
    let currentMenuKind = "";

    function hideMenu() {
        menu.classList.add("hidden");
        currentTableURL = null;
        currentMenuKind = "";
    }

    function menuItem(label, danger) {
        const b = document.createElement("button");
        b.className = "block w-full text-left px-3 py-1.5 hover:bg-gray-700 " +
            (danger ? "text-red-400 hover:text-red-300" : "");
        b.textContent = label;
        return b;
    }

    function openMenu(x, y, el) {
        menu.innerHTML = "";
        currentTableURL = null;
        currentMenuKind = el.getAttribute("data-tree-menu") || "";

        const btn = el.querySelector("button[hx-get]");
        if (btn) currentTableURL = btn.getAttribute("hx-get");

        // "Query Tool" opens an empty script tab connected to the
        // node's database (valid for database, schema and table).
        const qtItem = menuItem("Query Tool", false);
        qtItem.addEventListener("click", () => {
            const conn = connectionFromTreeURL(currentTableURL);
            if (conn) {
                openQueryToolTab(conn.serverID, conn.serverName, conn.dbName);
            } else {
                openTab("Query Tool");
            }
        });
        menu.appendChild(qtItem);

        menu.appendChild(menuItem("Delete", true));

        // The "Scripts" submenu generates DDL/DML scripts. Tables get
        // the full set, views get CREATE, INSERT and SELECT.
        if (currentMenuKind === "table" || currentMenuKind === "view") {
            const divider = document.createElement("div");
            divider.className = "my-1 border-t border-gray-700";
            menu.appendChild(divider);

            const row = document.createElement("div");
            row.className = "relative group";
            const trigger = document.createElement("button");
            trigger.className = "w-full text-left px-3 py-1.5 hover:bg-gray-700 flex items-center justify-between";
            trigger.innerHTML = '<span>Scripts</span><span class="text-xs text-gray-500">\u25B8</span>';
            const sub = document.createElement("div");
            sub.className = "absolute left-full top-0 hidden group-hover:block bg-gray-800 border border-gray-600 rounded shadow-xl py-1 min-w-[12rem]";
            const scripts = currentMenuKind === "table"
                ? ["CREATE Script", "DELETE Script", "INSERT Script",
                   "SELECT Script", "UPDATE Script"]
                : ["CREATE Script", "INSERT Script", "SELECT Script"];
            scripts.forEach(
                (label) => {
                    const item = menuItem(label, false);
                    item.addEventListener("click", () => {
                        if (label === "SELECT Script" && currentTableURL) {
                            openSelectScriptTab(currentTableURL);
                        } else if (label === "CREATE Script" && currentTableURL) {
                            openCreateScriptTab(currentTableURL);
                        } else if (label === "INSERT Script" && currentTableURL) {
                            openInsertScriptTab(currentTableURL);
                        } else if (label === "DELETE Script" && currentTableURL) {
                            openDeleteScriptTab(currentTableURL);
                        } else {
                            openTab(label);
                        }
                    });
                    sub.appendChild(item);
                });
            row.append(trigger, sub);
            menu.appendChild(row);
        }

        menu.classList.remove("hidden");

        // Keep the menu inside the viewport.
        menu.style.left = "0px";
        menu.style.top = "0px";
        const rect = menu.getBoundingClientRect();
        menu.style.left = Math.max(0, Math.min(x, window.innerWidth - rect.width - 4)) + "px";
        menu.style.top = Math.max(0, Math.min(y, window.innerHeight - rect.height - 4)) + "px";
    }

    document.addEventListener("contextmenu", (e) => {
        const el = e.target.closest("[data-tree-menu]");
        if (!el) {
            hideMenu();
            return;
        }
        e.preventDefault();
        openMenu(e.clientX, e.clientY, el);
    });
    document.addEventListener("click", hideMenu);
    document.addEventListener("keydown", (e) => {
        if (e.key === "Escape") hideMenu();
    });
    window.addEventListener("blur", hideMenu);
}
document.addEventListener("DOMContentLoaded", initContextMenu);