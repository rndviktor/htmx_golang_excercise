package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	pgdb "htmx-golang-excercise/internal/sqlc/postgres/db"
)

// handleCreateScript generates a pgAdmin-style CREATE TABLE script for a table
// and returns it as JSON {query: "..."} so the client can open a new script
// tab pre-filled with it.
func (s *Server) handleCreateScript(w http.ResponseWriter, r *http.Request) {
	pool, _, _, schemaName, tableName, ok := s.loadTablePool(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	queries := pgdb.New(pool)

	cols, err := queries.GetTableColumnsDetailed(ctx, pgdb.GetTableColumnsDetailedParams{
		Column1: pgtype.Text{String: schemaName, Valid: true},
		Column2: pgtype.Text{String: tableName, Valid: true},
	})
	if err != nil {
		http.Error(w, "Failed to query columns: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pkRows, err := queries.GetPrimaryKeyColumns(ctx, pgdb.GetPrimaryKeyColumnsParams{
		Nspname: schemaName,
		Relname: tableName,
	})
	if err != nil {
		http.Error(w, "Failed to query primary key: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tableInfo, err := queries.GetTableInfo(ctx, pgdb.GetTableInfoParams{
		Schemaname: schemaName,
		Tablename:  tableName,
	})
	owner := ""
	tablespace := "pg_default"
	if err == nil {
		owner = tableInfo.Tableowner
		if tableInfo.Tablespace.Valid && tableInfo.Tablespace.String != "" {
			tablespace = tableInfo.Tablespace.String
		}
	}

	var b strings.Builder
	qualified := qualIdent(schemaName, tableName)

	b.WriteString("-- Table: " + qualified + "\n\n")
	b.WriteString("-- DROP TABLE IF EXISTS " + qualified + ";\n\n")
	b.WriteString("CREATE TABLE IF NOT EXISTS " + qualified + "\n(\n")

	for i, col := range cols {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString("    " + columnLine(col))
	}

	if len(pkRows) > 0 {
		if len(cols) > 0 {
			b.WriteString(",\n")
		}
		names := make([]string, 0, len(pkRows))
		for _, r := range pkRows {
			names = append(names, quoteIfNeeded(r.Attname))
		}
		b.WriteString("    CONSTRAINT " + quoteIfNeeded(pkRows[0].Conname) +
			" PRIMARY KEY (" + strings.Join(names, ", ") + ")")
	}

	b.WriteString("\n)\n\n")
	b.WriteString("TABLESPACE " + quoteIfNeeded(tablespace) + ";\n\n")

	if owner != "" {
		b.WriteString("ALTER TABLE IF EXISTS " + qualified + "\n")
		b.WriteString("    OWNER to " + quoteIfNeeded(owner) + ";")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"query": b.String()})
}

// columnLine renders one CREATE TABLE column definition from its metadata,
// matching pgAdmin's formatting: serial detection, collation, NOT NULL and
// DEFAULT clauses.
func columnLine(col pgdb.GetTableColumnsDetailedRow) string {
	dataType := getString(col.DataType)
	if maxLen := optString(col.CharacterMaximumLength); maxLen != "" {
		switch dataType {
		case "character varying":
			dataType = "character varying(" + maxLen + ")"
		case "character":
			dataType = "character(" + maxLen + ")"
		}
	}

	def := optString(col.ColumnDefault)
	isSerial := false
	if def != "" && nextvalPattern.MatchString(def) {
		switch dataType {
		case "integer":
			dataType = "serial"
			isSerial = true
		case "bigint":
			dataType = "bigserial"
			isSerial = true
		case "smallint":
			dataType = "smallserial"
			isSerial = true
		}
	}

	s := quoteIfNeeded(getString(col.ColumnName)) + " " + dataType

	if coll := getString(col.Collation); coll != "" {
		s += " COLLATE pg_catalog." + quoteIdent(coll)
	}

	if getString(col.IsNullable) == "NO" {
		s += " NOT NULL"
	}

	if def != "" && !isSerial {
		s += " DEFAULT " + def
	}

	return s
}

// qualIdent joins a schema and table name into a qualification string,
// quoting either part only when required.
func qualIdent(schema, table string) string {
	return quoteIfNeeded(schema) + "." + quoteIfNeeded(table)
}

// quoteIdent wraps an identifier in double quotes, escaping embedded quotes.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

var safeIdent = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// quoteIfNeeded returns s as-is when it is a safe lowercase identifier that
// needs no quoting, otherwise it double-quotes it.
func quoteIfNeeded(s string) string {
	if safeIdent.MatchString(s) {
		return s
	}
	return quoteIdent(s)
}

var nextvalPattern = regexp.MustCompile(`^nextval\(.*'::regclass\)$`)

// optString formats a possibly-nil sqlc value as a string, returning "" for
// NULL.
func optString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
