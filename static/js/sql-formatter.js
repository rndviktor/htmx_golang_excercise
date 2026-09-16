/**
 * Minimal SQL formatter — PostgreSQL-aware, ~3 KB source.
 * Covers the subset used by the Format SQL button (indent + line breaks).
 * Handles: strings ('..', $$..$$), identifiers (".."), comments (--, /*),
 * subqueries, CTEs, CASE, and common DML/DDL keywords.
 */

const DDL_DML = new Set([
  "SELECT","FROM","WHERE","GROUP","ORDER","HAVING","LIMIT","OFFSET",
  "FETCH","FIRST","NEXT","ROW","ROWS","ONLY","FOR","UPDATE",
  "INSERT","INTO","VALUES","UPDATE","SET","DELETE","CREATE","ALTER",
  "DROP","WITH","RECURSIVE","RETURNING","EXPLAIN","ANALYZE","USING",
  "GRANT","REVOKE","ON","TO","CASCADE","RESTRICT","IF","EXISTS",
]);

const JOIN = new Set(["JOIN","LEFT","RIGHT","INNER","OUTER","CROSS","FULL","NATURAL"]);
const BOOL_OP = new Set(["AND","OR"]);
const BREAK_BEFORE = new Set([
  "SELECT","FROM","WHERE","GROUP","ORDER","HAVING","LIMIT","OFFSET",
  "FETCH","INSERT","VALUES","UPDATE","SET","DELETE","CREATE","ALTER",
  "DROP","WITH","RETURNING","UNION","INTERSECT","EXCEPT",
  "TABLESPACE","OWNER",
]);
const SELECT_LIST_END = new Set(["FROM","INTO","JOIN","LEFT","RIGHT","INNER","OUTER","CROSS","FULL","NATURAL"]);
const INLINE_PAREN_AFTER = new Set(["IN","ANY","SOME","ALL","ARRAY","EXCEPT","INTERSECT","LIKE","ILIKE","BETWEEN","OVER","PARTITION","ROW","ROWS","KEY"]);

// ── Tokeniser ────────────────────────────────────────────────────────

export function tokenize(sql) {
  const out = [];
  let i = 0;
  const len = sql.length;

  const emit = (type, value) => { out.push({ type, value }); };

  while (i < len) {
    const c = sql[i];

    // Whitespace
    if (c === " " || c === "\t") {
      let s = i; while (i < len && (sql[i] === " " || sql[i] === "\t")) i++;
      emit("ws", sql.slice(s, i));
      continue;
    }
    if (c === "\n" || c === "\r") {
      if (sql[i] === "\r" && sql[i + 1] === "\n") i++;
      i++;
      emit("ws", "\n");
      continue;
    }

    // Line comment
    if (c === "-" && sql[i + 1] === "-") {
      let s = i; i += 2;
      while (i < len && sql[i] !== "\n") i++;
      emit("comment", sql.slice(s, i));
      continue;
    }

    // Block comment
    if (c === "/" && sql[i + 1] === "*") {
      let s = i; i += 2;
      while (i < len && !(sql[i] === "*" && sql[i + 1] === "/")) i++;
      i += 2;
      emit("comment", sql.slice(s, i));
      continue;
    }

    // Dollar-quoted string
    if (c === "$") {
      let tag = "$"; i++;
      while (i < len && sql[i] !== "$") { tag += sql[i]; i++; }
      i++; // closing $
      let body = tag; // includes both dollar signs
      while (i < len) {
        if (sql[i] === "$") {
          let end = "$"; i++;
          while (i < len && sql[i] !== "$") { end += sql[i]; i++; }
          i++;
          body += end;
          if (end === tag) break;
        } else {
          body += sql[i]; i++;
        }
      }
      emit("string", body);
      continue;
    }

    // Quoted identifier
    if (c === '"') {
      let s = i; i++;
      while (i < len && sql[i] !== '"') { if (sql[i] === "\\") i++; i++; }
      i++;
      emit("ident", sql.slice(s, i));
      continue;
    }

    // String literal
    if (c === "'") {
      let s = i; i++;
      while (i < len) {
        if (sql[i] === "'") { if (sql[i + 1] === "'") { i += 2; } else { i++; break; } }
        else i++;
      }
      emit("string", sql.slice(s, i));
      continue;
    }

    // Number
    if (c >= "0" && c <= "9") {
      let s = i;
      while (i < len && ((sql[i] >= "0" && sql[i] <= "9") || sql[i] === ".")) i++;
      emit("number", sql.slice(s, i));
      continue;
    }

    // Semicolon
    if (c === ";") { i++; emit("semi", ";"); continue; }

    // Parens
    if (c === "(") { i++; emit("lparen", "("); continue; }
    if (c === ")") { i++; emit("rparen", ")"); continue; }

    // Comma
    if (c === ",") { i++; emit("comma", ","); continue; }

    // Operators
    if ("=<>!+*/%|&^~@".includes(c)) {
      let s = i;
      // Handle multi-char operators: :: -> ->> <> != <= >= || <>
      while (i < len && "=<>!+*/%|&^~@".includes(sql[i])) i++;
      emit("op", sql.slice(s, i));
      continue;
    }

    // Dot
    if (c === ".") { i++; emit("dot", "."); continue; }

    // Colon
    if (c === ":") { i++; if (sql[i] === ":") i++; emit("op", sql.slice(i === len ? i - 1 : i - (sql[i - 1] === ":" ? 2 : 1), i)); continue; }

    // Bracket (array syntax etc)
    if (c === "{") { i++; emit("lbrace", "{"); continue; }
    if (c === "}") { i++; emit("rbrace", "}"); continue; }
    if (c === "[") { i++; emit("lbracket", "["); continue; }
    if (c === "]") { i++; emit("rbracket", "]"); continue; }

    // Identifier / keyword (unquoted)
    if ((c >= "a" && c <= "z") || (c >= "A" && c <= "Z") || c === "_") {
      let s = i;
      while (i < len && ((sql[i] >= "a" && sql[i] <= "z") || (sql[i] >= "A" && sql[i] <= "Z") || (sql[i] >= "0" && sql[i] <= "9") || sql[i] === "_")) i++;
      const word = sql.slice(s, i);
      const upper = word.toUpperCase();
      if (DDL_DML.has(upper) || JOIN.has(upper) || BOOL_OP.has(upper)
        || ["UNION","INTERSECT","EXCEPT","ALL","DISTINCT","AS","NOT","IN",
            "BETWEEN","LIKE","ILIKE","SIMILAR","IS","NULL","TRUE","FALSE",
            "ANY","SOME","EXISTS","CASE","WHEN","THEN","ELSE","END",
            "ARRAY","PRIMARY","KEY","REFERENCES","DEFAULT","COLLATE",
            "CONFLICT","DO","NOTHING","OVER","PARTITION","ROW_NUMBER",
            "RANK","DENSE_RANK","LEAD","LAG","FIRST_VALUE","LAST_VALUE",
            "NTH_VALUE","ROWS","RANGE","GROUPS","UNBOUNDED","PRECEDING",
            "FOLLOWING","CURRENT","ROW","EXCLUDE","TIES","NO","PRESERVE",
            "SEARCH","DEPTH","BREADTH","CYCLE","RECURSIVE","LATERAL","BY",
            "MATERIALIZED","VERBOSE","ANALYZE","FORMAT","JSON","XML",
            "TEXT","BINARY","CSV","HEADER","DELIMITER","ENCODING",
            "FORCE","PARALLEL","ENABLE","DISABLE","TRIGGER","FUNCTION",
            "PROCEDURE","EXTENSION","SCHEMA","DATABASE","TABLE","INDEX",
            "VIEW","SEQUENCE","TYPE","RULE","OWNER","ROLE","TEMPORARY",
            "TABLESPACE",
            "TEMP","UNLOGGED","IF","THEN","ELSE","ELSIF","LOOP","WHILE",
            "FOR","FOREACH","REVERSE","EXIT","CONTINUE","RETURN","RAISE",
            "NOTICE","EXCEPTION","BEGIN","DECLARE","EXCEPTION","END",
            "PERFORM","EXECUTE","INTO","STRICT","FOUND","DIAGNOSTICS",
            "GET","CURRENT_CONDITION","PG_EXCEPTION_CONTEXT",
            "PG_EXCEPTION_DETAIL","PG_EXCEPTION_HINT",
          ].includes(upper)) {
        emit("keyword", word);
      } else {
        emit("ident", word);
      }
      continue;
    }

    // Anything else
    emit("other", c); i++;
  }

  return out;
}

// ── Formatter ────────────────────────────────────────────────────────

export function format(sql, opts = {}) {
  if (!sql || !sql.trim()) return sql || "";
  const tabW   = opts.tabWidth ?? 4;
  const useTab = opts.useTabs ?? false;
  const tab    = useTab ? "\t" : " ".repeat(tabW);

  const tokens = tokenize(sql);
  const out = [];
  let indent = 0;
  let lineStart = true;        // at start of a line (no chars emitted yet)
  let forbidSpace = false;     // next emitted token must not get a leading space
  let inSelect = false;        // currently inside a SELECT field list
  let inCase = 0;              // CASE depth
  let pendingBY = false;       // last keyword was GROUP/ORDER/PARTITION
  let seenCreate = false;      // saw CREATE in the current statement
  let pendingBlockParen = false; // next ( is a CREATE TABLE column list
  const parenNest = [];        // { inline: bool, blockList: bool, inSelect: bool }

  const indentStr = () => tab.repeat(Math.max(indent, 0));

  const pushNL = () => {
    if (lineStart) return; // avoid consecutive blank lines from input
    out.push("\n" + indentStr());
    lineStart = true;
  };

  const emit = (s) => {
    if (!lineStart && !forbidSpace) out.push(" ");
    out.push(s);
    lineStart = false;
    forbidSpace = false;
  };

  // Space that should definitely appear (e.g. " THEN ")
  const emitWord = (s, after = false) => {
    if (!lineStart) out.push(" ");
    out.push(s);
    lineStart = false;
    if (after) { out.push(" "); forbidSpace = true; }
    else forbidSpace = false;
  };

  const prevNonWs = (idx) => {
    for (let j = idx - 1; j >= 0; j--) if (tokens[j].type !== "ws") return tokens[j];
    return null;
  };
  const prevNonWsType = (idx) => (prevNonWs(idx) || {}).type;
  const prevNonWsUp = (idx) => {
    const t = prevNonWs(idx);
    if (!t) return null;
    return t.type === "keyword" || t.type === "ident" ? t.value.toUpperCase() : null;
  };

  for (let idx = 0; idx < tokens.length; idx++) {
    const tok = tokens[idx];
    const up  = tok.type === "keyword" ? tok.value.toUpperCase() : null;

    // ── Skip whitespace tokens; we control spacing ourselves ──────
    if (tok.type === "ws") continue;

    // ── Comments: always after a newline ─────────────────────────
    if (tok.type === "comment") {
      if (!lineStart) pushNL();
      out.push(tok.value);
      lineStart = false;
      continue;
    }

    // ── Semicolons: newline + blank line between queries ─────────
    if (tok.type === "semi") {
      out.push(";");
      indent = 0;
      seenCreate = false;
      pendingBlockParen = false;
      const gap = Math.max((opts.linesBetweenQueries ?? 2) - 1, 0);
      for (let b = 0; b < gap; b++) out.push("\n");
      out.push("\n");
      lineStart = true;
      continue;
    }

    // ── Dot: never surrounded by spaces ──────────────────────────
    if (tok.type === "dot") {
      out.push(".");
      lineStart = false;
      forbidSpace = true;
      continue;
    }

    // ── FROM / JOIN / INTO: close the select list, break ─────────
    if (up && SELECT_LIST_END.has(up)) {
      const closing = inSelect || up !== "INTO";
      if (closing) {
        if (inSelect) { inSelect = false; indent--; }
        if (!lineStart) pushNL();
        out.push(tok.value);
        lineStart = false;
        forbidSpace = false;
        continue;
      }
      // INTO without a select list (INSERT INTO): stay inline
      emit(tok.value);
      continue;
    }

    // ── Keywords that trigger a newline before ───────────────────
    if (up && BREAK_BEFORE.has(up)) {
      if (!lineStart) pushNL();

      if (up === "CREATE") seenCreate = true;

      if (up === "GROUP" || up === "ORDER" || up === "PARTITION") {
        out.push(tok.value);
        lineStart = false;
        pendingBY = true;
        continue;
      }
      if (up === "SELECT") {
        out.push("SELECT");
        inSelect = true;
        indent++;
        lineStart = false;
        pushNL();
        continue;
      }
      out.push(tok.value);
      lineStart = false;
      continue;
    }

    // ── BY (keeps ORDER BY / GROUP BY on one line) ───────────────
    if (up === "BY" && pendingBY) {
      out.push(" BY");
      lineStart = false;
      pendingBY = false;
      continue;
    }

    if (up === "ON") {
      if (!lineStart) pushNL();
      out.push("ON");
      lineStart = false;
      continue;
    }

    // ── AND / OR: break before ───────────────────────────────────
    if (BOOL_OP.has(up)) {
      if (!lineStart) pushNL();
      out.push(tok.value);
      lineStart = false;
      continue;
    }

    // ── CASE / WHEN / THEN / ELSE / END ──────────────────────────
    if (up === "CASE") {
      if (!lineStart) pushNL();
      out.push("CASE");
      inCase++;
      indent++;
      lineStart = false;
      continue;
    }
    if (up === "WHEN" || up === "ELSE") {
      if (!lineStart) pushNL();
      emitWord(tok.value);
      continue;
    }
    if (up === "THEN") {
      if (!lineStart) pushNL();
      emitWord("THEN", true);
      continue;
    }
    if (up === "END") {
      if (inCase > 0) { inCase--; indent--; }
      if (!lineStart) pushNL();
      out.push("END");
      lineStart = false;
      continue;
    }

    // ── Left paren: inline (function call / list) or block ───────
    if (tok.type === "lparen") {
      const prevType = prevNonWsType(idx);
      const prevUp = prevNonWsUp(idx);
      const isCreateList = pendingBlockParen;
      pendingBlockParen = false;
      const inline = !isCreateList && (prevType === "ident" || (prevUp !== null && INLINE_PAREN_AFTER.has(prevUp)) || prevType === "string" || prevType === "number" || prevType === "rparen" || prevType === "other");

      if (isCreateList) {
        // CREATE TABLE column list: opening ( on its own line, one column per
        // line beneath it, closing ) back at column 0.
        if (!lineStart) out.push("\n" + indentStr());
        out.push("(");
        parenNest.push({ inline: false, blockList: true, inSelect });
        indent++;
        pushNL();
        continue;
      }

      indent++;
      parenNest.push({ inline, blockList: false, inSelect });
      if (inline) {
        if (prevUp === "KEY") out.push(" ");
        out.push("(");
        lineStart = false;
        forbidSpace = true;
      } else {
        if (!forbidSpace && !lineStart) out.push(" ");
        out.push("(");
        pushNL();
      }
      continue;
    }

    // ── Right paren ──────────────────────────────────────────────
    if (tok.type === "rparen") {
      const frame = parenNest.pop() || { inline: true, inSelect: false };
      if (indent > 0) indent--;
      inSelect = frame.inSelect;
      if (!frame.inline && !lineStart) pushNL();
      out.push(")");
      lineStart = false;
      forbidSpace = false;
      continue;
    }

    // ── Comma ────────────────────────────────────────────────────
    if (tok.type === "comma") {
      out.push(",");
      const top = parenNest[parenNest.length - 1];
      if ((inSelect && parenNest.length === 0) || (top && top.blockList)) {
        pushNL();
        continue;
      }
      out.push(" ");
      forbidSpace = true;
      lineStart = false;
      continue;
    }

    // ── Everything else (ident, number, string, op) ──────────────
    if (up === "TABLE" && seenCreate) pendingBlockParen = true;
    if (up === "AS") pendingBlockParen = false;

    if (tok.type === "op") {
      // Casts and json arrows hug their operand; other operators get spaces
      const tight = /^[:]|^->/.test(tok.value);
      if (tight && forbidSpace) { /* still tight */ }
      emit(tok.value);
      // after a tight operator, the next token must also hug (id::text)
      forbidSpace = tight;
      continue;
    }

    emit(tok.value);
  }

  // Clean up trailing whitespace on last line
  let result = out.join("");
  result = result.replace(/\s+$/gm, "");
  return result.trimEnd();
}