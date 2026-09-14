// CodeMirror 6 SQL editor: syntax highlighting + autocompletion. Loads
// via esm.sh so every package shares one module graph (no duplicate
// EditorState instances). Exposes window.SqlEditor (init/value/set/
// selection/focus) used by the classic-script tab logic above.
import { EditorView, keymap, lineNumbers, highlightActiveLineGutter, drawSelection, dropCursor, rectangularSelection, crosshairCursor, placeholder } from "https://esm.sh/@codemirror/view@6";
import { EditorState } from "https://esm.sh/@codemirror/state@6";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "https://esm.sh/@codemirror/commands@6";
import { bracketMatching, indentOnInput, syntaxHighlighting, HighlightStyle } from "https://esm.sh/@codemirror/language@6";
import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap, ifNotIn } from "https://esm.sh/@codemirror/autocomplete@6";
import { highlightSelectionMatches, searchKeymap } from "https://esm.sh/@codemirror/search@6";
import { sql, PostgreSQL, keywordCompletionSource } from "https://esm.sh/@codemirror/lang-sql@6";
import { tags } from "https://esm.sh/@lezer/highlight@1";

// Dark theme matching the Tailwind gray-900 palette used elsewhere.
const editorTheme = EditorView.theme({
    "&": { height: "100%", backgroundColor: "#111827", color: "#e5e7eb", fontSize: "13px" },
    ".cm-content": {
        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
        padding: "10px 0 10px 4px",
        caretColor: "#93c5fd",
    },
    ".cm-scroller": { lineHeight: "1.55" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "#93c5fd", borderLeftWidth: "2px" },
    "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection": {
        backgroundColor: "rgba(59, 130, 246, 0.30)",
    },
    ".cm-activeLine": { backgroundColor: "rgba(75, 85, 99, 0.20)" },
    ".cm-gutters": { backgroundColor: "#1f2937", color: "#6b7280", borderRight: "1px solid #374151" },
    ".cm-activeLineGutter": { backgroundColor: "rgba(59, 130, 246, 0.15)", color: "#e5e7eb" },
    "&.cm-focused": { outline: "none" },
}, { dark: true });

// SQL token colors (keyword/type/string/comment/...).
const sqlHighlight = HighlightStyle.define([
    { tag: tags.keyword, color: "#93c5fd", fontWeight: "600" },
    { tag: tags.typeName, color: "#7dd3fc" },
    { tag: tags.standard(tags.name), color: "#c4b5fd" },
    { tag: tags.number, color: "#fcd34d" },
    { tag: tags.bool, color: "#fca5a5" },
    { tag: tags.null, color: "#fca5a5" },
    { tag: tags.string, color: "#86efac" },
    { tag: tags.comment, color: "#6b7280", fontStyle: "italic" },
    { tag: tags.name, color: "#d7dde6" },
    { tag: tags.operator, color: "#c7d2fe" },
    { tag: tags.punctuation, color: "#9ca3af" },
    { tag: tags.paren, color: "#f9a8d4" },
    { tag: tags.brace, color: "#f9a8d4" },
    { tag: tags.squareBracket, color: "#f9a8d4" },
    { tag: tags.special(tags.string), color: "#86efac" },
    { tag: tags.special(tags.name), color: "#22d3ee" },
]);

// The connected database's schema ({schema: {table: [columns]}}),
// fetched per tab by loadSchemaCompletion. The completion sources
// below read it on every keystroke, so no state reconfiguration is
// needed — only this variable is updated when the schema arrives.
let currentSchema = null;

// Postgres keyword/type completions (upper-cased), replaces the
// language's own keyword source inside the autocompletion override.
const keywordCompletion = keywordCompletionSource(PostgreSQL, true);

// Completes bare table/column names from the DB schema (e.g. "cr|"
// offers created_at) and, after a dot, the members of the referenced
// schema/table ("schema. |" offers tables, "schema.table. |" and
// "table. |" offer columns). Registered via autocompletion's override
// option so its results are reliably merged with the keyword source.
function dbSchemaSource(context) {
    const before = context.matchBefore(/[\w$]*/);
    if (!before || (before.from === before.to && !context.explicit)) return null;
    const schema = currentSchema;
    if (!schema) return null;

    const lineStart = context.state.doc.lineAt(before.from).from;
    const dottedPrefix = /(?:[\w$]+\s*\.\s*)+$/.exec(
        context.state.sliceDoc(lineStart, before.from));

    if (dottedPrefix) {
        const parts = dottedPrefix[0].split(/\.\s*/).map((s) => s.trim());
        if (parts.length >= 2) {
            const [sch, tbl] = parts.slice(-2);
            const cols = (schema[sch] || {})[tbl];
            if (!cols) return null;
            return {
                from: before.from,
                options: cols.map((c) => ({ label: c, type: "variable" })),
                validFor: /^[\w$]*$/,
            };
        }
        const tables = schema[parts[0]];
        if (tables) {
            return {
                from: before.from,
                options: Object.keys(tables).map((t) => ({ label: t, type: "property", detail: parts[0] })),
                validFor: /^[\w$]*$/,
            };
        }
        // Not a schema name: treat as a bare table (or alias equal to
        // a table name) and offer that table's columns.
        for (const tableMap of Object.values(schema)) {
            const cols = tableMap[parts[0]];
            if (cols) {
                return {
                    from: before.from,
                    options: cols.map((c) => ({ label: c, type: "variable" })),
                    validFor: /^[\w$]*$/,
                };
            }
        }
        return null;
    }

    const tables = [];
    const columns = [];
    const seenTable = new Set();
    const seenColumn = new Set();
    for (const [schemaName, tableMap] of Object.entries(schema)) {
        for (const [tableName, cols] of Object.entries(tableMap)) {
            const key = schemaName + "." + tableName;
            if (!seenTable.has(key)) {
                seenTable.add(key);
                tables.push({ label: tableName, type: "property", detail: schemaName, boost: 10 });
            }
            for (const col of cols) {
                if (!seenColumn.has(col)) {
                    seenColumn.add(col);
                    columns.push({ label: col, type: "variable", detail: tableName + " (" + schemaName + ")" });
                }
            }
        }
    }
    return { from: before.from, options: tables.concat(columns), validFor: /^[\w$]*$/ };
}

const schemaCompletion = ifNotIn(["QuotedIdentifier", "String", "LineComment", "BlockComment"], dbSchemaSource);

function runCurrentQuery(panel) {
    const execBtn = panel.querySelector('[onclick="executeQuery(this)"]');
    if (execBtn && typeof window.executeQuery === "function") window.executeQuery(execBtn);
    return true;
}

function editorExtensions(panel) {
    return [
        sql({ dialect: PostgreSQL, upperCaseKeywords: true }),
        lineNumbers(),
        highlightActiveLineGutter(),
        history(),
        drawSelection(),
        dropCursor(),
        rectangularSelection(),
        crosshairCursor(),
        indentOnInput(),
        bracketMatching(),
        closeBrackets(),
        autocompletion({ override: [keywordCompletion, schemaCompletion] }),
        highlightSelectionMatches(),
        syntaxHighlighting(sqlHighlight),
        editorTheme,
        EditorState.allowMultipleSelections.of(true),
        EditorView.updateListener.of((update) => {
            if (update.docChanged && typeof window.scheduleSave === "function") window.scheduleSave();
        }),
        keymap.of([
            ...closeBracketsKeymap,
            ...defaultKeymap,
            ...searchKeymap,
            ...historyKeymap,
            ...completionKeymap,
            indentWithTab,
            { key: "F5", run: () => runCurrentQuery(panel), preventDefault: true },
            { key: "Mod-Enter", run: () => runCurrentQuery(panel), preventDefault: true },
        ]),
        placeholder("Enter SQL here..."),
    ];
}

// Fetches the connected database's schema
// ({default_schema, schemas: {schema: {table: [cols]}}}) and makes it
// available to the DB-aware table/column completion sources.
function loadSchemaCompletion(panel) {
    const params = formConnectionParams(queryForm(panel));
    if (!params || !params.sid.value || !params.db.value) return;

    fetch("/api/servers/" + encodeURIComponent(params.sid.value) +
          "/databases/" + encodeURIComponent(params.db.value) + "/autocomplete-schema")
        .then((r) => { if (!r.ok) throw new Error("HTTP " + r.status); return r.json(); })
        .then((data) => {
            currentSchema = data.schemas || data;
        })
        .catch((err) => console.error("Failed to load SQL schema for autocompletion:", err));
}

function initSqlEditor(panel) {
    const host = panel && panel.querySelector ? panel.querySelector("[data-sql-editor]") : null;
    if (!host || host._cm) return;
    const view = new EditorView({
        parent: host,
        extensions: editorExtensions(panel),
    });
    host._cm = view;
    loadSchemaCompletion(panel);
}

window.SqlEditor = {
    init: initSqlEditor,
    view(panel) {
        const host = panel && panel.querySelector ? panel.querySelector("[data-sql-editor]") : null;
        return host ? host._cm : null;
    },
    value(panel) {
        const view = this.view(panel);
        return view ? view.state.doc.toString() : "";
    },
    set(panel, text) {
        const view = this.view(panel);
        if (view) {
            view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: String(text) } });
        }
    },
    selection(panel) {
        const view = this.view(panel);
        if (!view) return { from: 0, to: 0 };
        const sel = view.state.selection.main;
        return { from: sel.from, to: sel.to };
    },
    focus(panel, sel) {
        const view = this.view(panel);
        if (!view) return;
        view.focus();
        if (sel) view.dispatch({ selection: { anchor: sel.from, head: sel.to } });
    },
};