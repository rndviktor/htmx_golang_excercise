package web

import (
	"encoding/json"
	"log"
	"net/http"
)

// autocompleteSchemaResponse is the JSON payload returned by
// handleAutocompleteSchema for the CodeMirror SQL completion.
type autocompleteSchemaResponse struct {
	// DefaultSchema is the connection's current schema (search_path), used by
	// CodeMirror's defaultSchema option so unqualified table names complete.
	DefaultSchema string `json:"default_schema"`
	// Schemas maps schema names to tables and their columns in the shape
	// CodeMirror's SQL completion expects: { schema: { table: [columns] } }.
	Schemas map[string]map[string][]string `json:"schemas"`
}

// handleAutocompleteSchema returns the connected database's schema as JSON for
// CodeMirror's SQL completion. User tables and views are included; system
// schemas (pg_catalog, information_schema and pg_*) are skipped to keep the
// payload small.
func (s *Server) handleAutocompleteSchema(w http.ResponseWriter, r *http.Request) {
	pool, _, _, ok := s.loadDatabasePool(w, r)
	if !ok {
		return
	}

	var defaultSchema string
	if err := pool.QueryRow(r.Context(), `SELECT current_schema()`).Scan(&defaultSchema); err != nil {
		log.Printf("Failed to read current schema: %v", err)
	}

	rows, err := pool.Query(r.Context(), `
		SELECT table_schema, table_name, column_name
		FROM information_schema.columns
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		  AND table_schema NOT LIKE 'pg\_%'
		ORDER BY table_schema, table_name, ordinal_position`)
	if err != nil {
		log.Printf("Failed to load autocomplete schema: %v", err)
		http.Error(w, "Failed to load schema", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	schema := make(map[string]map[string][]string)
	for rows.Next() {
		var schemaName, tableName, columnName string
		if err := rows.Scan(&schemaName, &tableName, &columnName); err != nil {
			log.Printf("Failed to scan autocomplete row: %v", err)
			http.Error(w, "Failed to load schema", http.StatusInternalServerError)
			return
		}
		tables := schema[schemaName]
		if tables == nil {
			tables = make(map[string][]string)
			schema[schemaName] = tables
		}
		tables[tableName] = append(tables[tableName], columnName)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Autocomplete schema iteration error: %v", err)
		http.Error(w, "Failed to load schema", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(autocompleteSchemaResponse{
		DefaultSchema: defaultSchema,
		Schemas:       schema,
	}); err != nil {
		log.Printf("Failed to encode autocomplete schema: %v", err)
	}
}
