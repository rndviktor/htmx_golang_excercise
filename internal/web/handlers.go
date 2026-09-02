package web

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"htmx-golang-excercise/internal/db"
	pgdb "htmx-golang-excercise/internal/sqlc/postgres/db"
	sqlite "htmx-golang-excercise/internal/sqlc/sqlite/db"
)

type Server struct {
	DB       *sqlite.Queries
	sqliteDB *sql.DB
}

func NewServer() (*Server, error) {
	database, queries, err := db.Open("pgadmin4.db")
	if err != nil {
		return nil, err
	}

	return &Server{DB: queries, sqliteDB: database}, nil
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Get("/login", s.handleLoginGet)
	r.Post("/login", s.handleLoginPost)
	r.Post("/logout", s.handleLogout)

	r.Group(func(r chi.Router) {
		r.Use(s.RequireAuth)

		r.Get("/", s.handleIndex)
		r.Get("/api/tree", s.handleTree)
		r.Get("/api/tabs/script-panel", s.handleScriptTabPanel)
		r.Get("/api/query-history", s.handleQueryHistory)
		r.Get("/api/query-history/{id}", s.handleQueryHistoryDetail)
		r.Post("/api/execute-query", s.handleExecuteQuery)

		r.Route("/api/servers", func(r chi.Router) {
			r.Get("/", s.handleServerList)
			r.Get("/new", s.handleNewServerModal)
			r.Post("/", s.handleAddServer)

			r.Route("/{serverID}", func(r chi.Router) {
				r.Get("/children", s.handleServerChildren)
				r.Get("/databases", s.handleServerDatabases)
				r.Get("/roles", s.handleServerRoles)
				r.Get("/tablespaces", s.handleServerTablespaces)

				r.Route("/databases/{dbName}", func(r chi.Router) {
					r.Get("/children", s.handleDatabaseChildren)
					r.Get("/{category}", s.handleDatabaseCategory)

					r.Route("/schemas/{schemaName}", func(r chi.Router) {
						r.Get("/children", s.handleSchemaChildren)
						r.Get("/{category}", s.handleSchemaCategory)

						r.Route("/tables/{tableName}", func(r chi.Router) {
							r.Get("/children", s.handleTableChildren)
							r.Get("/{category}", s.handleTableCategory)
							r.Get("/columns", s.handleTableColumns)
							r.Get("/create-script", s.handleCreateScript)
							r.Get("/insert-script", s.handleInsertScript)
							r.Get("/delete-script", s.handleDeleteScript)
						})
					})
				})
			})
		})

		r.Route("/api/workspace", func(r chi.Router) {
			r.Get("/", s.handleWorkspaceGet)
			r.Post("/", s.handleWorkspaceSave)
		})
	})

	return r
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	Render(w, "index.html", map[string]any{
		"Title":         "The Main Dashboard",
		"Authenticated": true,
		"Username":      GetAuthenticatedUser(r),
	})
}

// handleTree renders the root of the object explorer: the server group node,
// labeled with the number of registered servers. Its children are
// lazy-loaded on click via GET /api/servers.
func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	servers, ok := s.listServers(w, r)
	if !ok {
		return
	}

	badge := ""
	if len(servers) > 0 {
		badge = strconv.Itoa(len(servers))
	}

	renderTree(w, []treeNode{{
		ID:    "servers-group",
		Icon:  "🐘",
		Label: "Servers",
		Badge: badge,
		URL:   "/api/servers",
	}}, "")
}

func (s *Server) handleServerList(w http.ResponseWriter, r *http.Request) {
	servers, ok := s.listServers(w, r)
	if !ok {
		return
	}

	nodes := make([]treeNode, 0, len(servers))
	for _, srv := range servers {
		nodes = append(nodes, treeNode{
			ID:    fmt.Sprintf("server-%d", srv.ID),
			Icon:  "🖥️",
			Label: srv.Name,
			Sub:   fmt.Sprintf("%s:%d / %s", srv.Host, srv.Port, srv.MaintenanceDb),
			URL:   fmt.Sprintf("/api/servers/%d/children", srv.ID),
		})
	}

	renderTree(w, nodes, "No servers registered yet.")
}

// handleServerChildren renders the category folders shown when a server node
// is expanded in the tree.
func (s *Server) handleServerChildren(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "serverID"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "Invalid server id", http.StatusBadRequest)
		return
	}

	if _, err := s.DB.GetServerByID(r.Context(), sqlite.GetServerByIDParams{
		ID:     id,
		UserID: db.DefaultUserID,
	}); err != nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	base := fmt.Sprintf("/api/servers/%d", id)
	renderTree(w, []treeNode{
		expander(fmt.Sprintf("server-%d-databases", id), "🗃️", "Databases", base+"/databases"),
		expander(fmt.Sprintf("server-%d-roles", id), "👥", "Login/Group Roles", base+"/roles"),
		expander(fmt.Sprintf("server-%d-tablespaces", id), "💽", "Tablespaces", base+"/tablespaces"),
	}, "")
}

// loadServerPool validates the {serverID} route param and returns a live
// pgx pool for that registered server. On failure it writes the error
// response itself and returns ok=false.
func (s *Server) loadServerPool(w http.ResponseWriter, r *http.Request) (*pgxpool.Pool, int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "serverID"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "Invalid server id", http.StatusBadRequest)
		return nil, 0, false
	}

	pool, err := s.getOrCreatePool(r.Context(), id)
	if err != nil {
		log.Printf("Failed to connect to server %d: %v", id, err)
		http.Error(w, "Cannot connect to server: "+err.Error(), http.StatusBadGateway)
		return nil, 0, false
	}

	return pool, id, true
}

// handleServerDatabases lists the databases of a registered server queried
// live from its maintenance database.
func (s *Server) handleServerDatabases(w http.ResponseWriter, r *http.Request) {
	pool, id, ok := s.loadServerPool(w, r)
	if !ok {
		return
	}

	names, err := pgdb.New(pool).ListDatabases(r.Context())
	if err != nil {
		log.Printf("Failed to list databases: %v", err)
		http.Error(w, "Failed to load databases", http.StatusInternalServerError)
		return
	}

	nodes := make([]treeNode, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, treeNode{
			ID:    fmt.Sprintf("database-%d-%s", id, name),
			Icon:  "🗄️",
			Label: name,
			URL:   fmt.Sprintf("/api/servers/%d/databases/%s/children", id, name),
		})
	}

	renderTree(w, nodes, "No databases found.")
}

// handleDatabaseChildren renders the category folders shown when a database
// node is expanded in the tree.
func (s *Server) handleDatabaseChildren(w http.ResponseWriter, r *http.Request) {
	_, id, dbName, ok := s.loadDatabasePool(w, r)
	if !ok {
		return
	}

	renderTree(w, categoryFolders(
		fmt.Sprintf("database-%d-%s", id, dbName),
		fmt.Sprintf("/api/servers/%d/databases/%s", id, dbName),
		dbCategories,
	), "")
}

// handleDatabaseCategory lists the contents of one database category folder
// (casts, extensions, schemas, ...) queried live from that database.
func (s *Server) handleDatabaseCategory(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "category")
	cat := findCategory(dbCategories, slug)
	if cat == nil {
		http.Error(w, "Unknown database item category", http.StatusNotFound)
		return
	}

	pool, id, dbName, ok := s.loadDatabasePool(w, r)
	if !ok {
		return
	}

	names, err := cat.ListNames(r.Context(), pool)
	if err != nil {
		log.Printf("Failed to load %s: %v", cat.Label, err)
		http.Error(w, "Failed to load "+cat.Label, http.StatusInternalServerError)
		return
	}

	// Schemas are not leaves: each one is an expandable node whose children
	// are the schema's own object folders (tables, views, ...).
	if slug == "schemas" {
		nodes := make([]treeNode, 0, len(names))
		for _, name := range names {
			nodes = append(nodes, treeNode{
				ID:    fmt.Sprintf("schema-%d-%s-%s", id, dbName, name),
				Icon:  "🗂️",
				Label: name,
				URL:   fmt.Sprintf("/api/servers/%d/databases/%s/schemas/%s/children", id, dbName, name),
			})
		}
		renderTree(w, nodes, cat.Empty)
		return
	}

	renderTree(w, leaves(cat.Icon, names), cat.Empty)
}

// loadDatabasePool validates the {serverID}/{dbName} route params and returns
// a live pgx pool connected to that specific database. On failure it writes
// the error response itself and returns ok=false.
func (s *Server) loadDatabasePool(w http.ResponseWriter, r *http.Request) (*pgxpool.Pool, int64, string, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "serverID"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "Invalid server id", http.StatusBadRequest)
		return nil, 0, "", false
	}

	dbName := chi.URLParam(r, "dbName")
	if dbName == "" {
		http.Error(w, "Invalid database name", http.StatusBadRequest)
		return nil, 0, "", false
	}

	pool, err := s.getOrCreateDbPool(r.Context(), id, dbName)
	if err != nil {
		log.Printf("Failed to connect to server %d database %q: %v", id, dbName, err)
		http.Error(w, "Cannot connect to database: "+err.Error(), http.StatusBadGateway)
		return nil, 0, "", false
	}

	return pool, id, dbName, true
}

// handleSchemaChildren renders the object folders shown when a schema node is
// expanded in the tree.
func (s *Server) handleSchemaChildren(w http.ResponseWriter, r *http.Request) {
	_, id, dbName, schemaName, ok := s.loadSchemaPool(w, r)
	if !ok {
		return
	}

	renderTree(w, categoryFolders(
		fmt.Sprintf("schema-%d-%s-%s", id, dbName, schemaName),
		fmt.Sprintf("/api/servers/%d/databases/%s/schemas/%s", id, dbName, schemaName),
		schemaCategories,
	), "")
}

// handleSchemaCategory lists the contents of one folder inside a schema
// (tables, views, sequences, ...) queried live from that database.
func (s *Server) handleSchemaCategory(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "category")
	cat := findCategory(schemaCategories, slug)
	if cat == nil {
		http.Error(w, "Unknown schema item category", http.StatusNotFound)
		return
	}

	pool, id, dbName, schemaName, ok := s.loadSchemaPool(w, r)
	if !ok {
		return
	}

	names, err := cat.ListNames(r.Context(), pool, schemaName)
	if err != nil {
		log.Printf("Failed to load %s: %v", cat.Label, err)
		http.Error(w, "Failed to load "+cat.Label, http.StatusInternalServerError)
		return
	}

	// Tables are not leaves either: each one expands into its own object
	// folders (columns, constraints, indexes, ...) and carries a right-click
	// context menu.
	if slug == "tables" {
		nodes := make([]treeNode, 0, len(names))
		for _, name := range names {
			tablePath := fmt.Sprintf("/api/servers/%d/databases/%s/schemas/%s/tables/%s", id, dbName, schemaName, name)
			nodes = append(nodes, treeNode{
				ID:    fmt.Sprintf("table-%d-%s-%s-%s", id, dbName, schemaName, name),
				Icon:  cat.Icon,
				Label: name,
				URL:   tablePath + "/children",
				Menu:  "table",
			})
		}
		renderTree(w, nodes, cat.Empty)
		return
	}

	renderTree(w, leaves(cat.Icon, names), cat.Empty)
}

// loadSchemaPool validates the {serverID}/{dbName}/{schemaName} route params
// and returns a live pgx pool connected to that database. On failure it writes
// the error response itself and returns ok=false.
func (s *Server) loadSchemaPool(w http.ResponseWriter, r *http.Request) (*pgxpool.Pool, int64, string, string, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "serverID"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "Invalid server id", http.StatusBadRequest)
		return nil, 0, "", "", false
	}

	dbName := chi.URLParam(r, "dbName")
	schemaName := chi.URLParam(r, "schemaName")
	if dbName == "" || schemaName == "" {
		http.Error(w, "Invalid database or schema name", http.StatusBadRequest)
		return nil, 0, "", "", false
	}

	pool, err := s.getOrCreateDbPool(r.Context(), id, dbName)
	if err != nil {
		log.Printf("Failed to connect to server %d database %q: %v", id, dbName, err)
		http.Error(w, "Cannot connect to database: "+err.Error(), http.StatusBadGateway)
		return nil, 0, "", "", false
	}

	return pool, id, dbName, schemaName, true
}

// loadTablePool validates the {serverID}/{dbName}/{schemaName}/{tableName}
// route params and returns a live pgx pool connected to that database. On
// failure it writes the error response itself and returns ok=false.
func (s *Server) loadTablePool(w http.ResponseWriter, r *http.Request) (*pgxpool.Pool, int64, string, string, string, bool) {
	pool, id, dbName, schemaName, ok := s.loadSchemaPool(w, r)
	if !ok {
		return nil, 0, "", "", "", false
	}

	tableName := chi.URLParam(r, "tableName")
	if tableName == "" {
		http.Error(w, "Invalid table name", http.StatusBadRequest)
		return nil, 0, "", "", "", false
	}

	return pool, id, dbName, schemaName, tableName, true
}

// handleTableChildren renders the object folders shown when a table node is
// expanded in the tree.
func (s *Server) handleTableChildren(w http.ResponseWriter, r *http.Request) {
	// The pool is not needed here, but connecting validates the server and
	// database before the folders are rendered.
	_, id, dbName, schemaName, tableName, ok := s.loadTablePool(w, r)
	if !ok {
		return
	}

	renderTree(w, categoryFolders(
		fmt.Sprintf("table-%d-%s-%s-%s", id, dbName, schemaName, tableName),
		fmt.Sprintf("/api/servers/%d/databases/%s/schemas/%s/tables/%s", id, dbName, schemaName, tableName),
		tableCategories,
	), "")
}

// handleTableCategory lists the contents of one folder inside a table
// (columns, constraints, indexes, ...) queried live from that database.
func (s *Server) handleTableCategory(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "category")
	cat := findCategory(tableCategories, slug)
	if cat == nil {
		http.Error(w, "Unknown table item category", http.StatusNotFound)
		return
	}

	pool, _, _, schemaName, tableName, ok := s.loadTablePool(w, r)
	if !ok {
		return
	}

	names, err := cat.ListNames(r.Context(), pool, schemaName, tableName)
	if err != nil {
		log.Printf("Failed to load %s: %v", cat.Label, err)
		http.Error(w, "Failed to load "+cat.Label, http.StatusInternalServerError)
		return
	}

	renderTree(w, leaves(cat.Icon, names), cat.Empty)
}

func (s *Server) handleServerRoles(w http.ResponseWriter, r *http.Request) {
	pool, _, ok := s.loadServerPool(w, r)
	if !ok {
		return
	}

	names, err := pgdb.New(pool).ListRoles(r.Context())
	if err != nil {
		log.Printf("Failed to list roles: %v", err)
		http.Error(w, "Failed to load roles", http.StatusInternalServerError)
		return
	}

	renderTree(w, leaves("👤", names), "No roles found.")
}

func (s *Server) handleServerTablespaces(w http.ResponseWriter, r *http.Request) {
	pool, _, ok := s.loadServerPool(w, r)
	if !ok {
		return
	}

	names, err := pgdb.New(pool).ListTablespaces(r.Context())
	if err != nil {
		log.Printf("Failed to list tablespaces: %v", err)
		http.Error(w, "Failed to load tablespaces", http.StatusInternalServerError)
		return
	}

	renderTree(w, leaves("📀", names), "No tablespaces found.")
}

func (s *Server) handleNewServerModal(w http.ResponseWriter, r *http.Request) {
	RenderPartial(w, "add_server_modal.html", nil)
}

func (s *Server) handleScriptTabPanel(w http.ResponseWriter, r *http.Request) {
	RenderPartial(w, "script_tab_panel.html", nil)
}

func (s *Server) handleExecuteQuery(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	query := r.FormValue("sql_query")
	serverIDStr := r.FormValue("server_id")
	dbName := r.FormValue("db_name")
	tabID := r.FormValue("tab_id")

	if query == "" || serverIDStr == "" || dbName == "" {
		http.Error(w, "Missing query, server_id, or db_name", http.StatusBadRequest)
		return
	}

	serverID, err := strconv.ParseInt(serverIDStr, 10, 64)
	if err != nil || serverID < 1 {
		http.Error(w, "Invalid server id", http.StatusBadRequest)
		return
	}

	page, _ := strconv.Atoi(r.FormValue("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.FormValue("limit"))
	if limit < 1 {
		limit = 1000
	}
	offset := (page - 1) * limit

	query = strings.TrimRight(query, " \t\n\r;")

	pool, err := s.getOrCreateDbPool(r.Context(), serverID, dbName)
	if err != nil {
		http.Error(w, "Cannot connect to database: "+err.Error(), http.StatusBadGateway)
		return
	}

	start := time.Now()

	// Statements that return rows (SELECT and friends) can be wrapped for
	// count + pagination. Everything else (CREATE/DROP/ALTER/INSERT/UPDATE/
	// DELETE/...) is executed directly and returns a command tag instead.
	if !isRowReturning(query) {
		exec, err := pool.Exec(r.Context(), query)
		elapsed := time.Since(start).Seconds()

		message := "OK"
		var rowsAffected int64
		if err != nil {
			message = err.Error()
		} else {
			message = exec.String()
			rowsAffected = exec.RowsAffected()
		}

		s.recordQueryHistory(r.Context(), serverID, dbName, tabID, query, int64(elapsed*1000), rowsAffected)

		RenderPartial(w, "query_result.html", map[string]any{
			"Headers":    []string{},
			"Rows":       [][]string{},
			"Page":       1,
			"Total":      0,
			"TotalPages": 1,
			"Limit":      limit,
			"Offset":     0,
			"Elapsed":    elapsed,
			"Message":    message,
		})
		return
	}

	// Send the count query and the paginated fetch in a single batch so they
	// execute on one connection in one network round trip instead of two.
	batch := &pgx.Batch{}
	batch.Queue("SELECT COUNT(*) FROM ("+query+") _cnt")
	batch.Queue("SELECT * FROM ("+query+") _q LIMIT $1 OFFSET $2", limit, offset)

	conn, err := pool.Acquire(r.Context())
	if err != nil {
		http.Error(w, "Cannot connect to database: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer conn.Release()

	br := conn.SendBatch(r.Context(), batch)
	defer br.Close()

	// Count total rows
	var total int
	if err := br.QueryRow().Scan(&total); err != nil {
		http.Error(w, "Count query failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Fetch paginated rows
	rows, err := br.Query()
	if err != nil {
		http.Error(w, "Query failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer rows.Close()

	fieldDescriptions := rows.FieldDescriptions()
	colCount := len(fieldDescriptions)

	var headers []string
	for _, fd := range fieldDescriptions {
		headers = append(headers, fd.Name)
	}

	var rowsData [][]string
	for rows.Next() {
		vals := make([]any, colCount)
		valPtrs := make([]any, colCount)
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		if err := rows.Scan(valPtrs...); err != nil {
			http.Error(w, "Row scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		row := make([]string, colCount)
		for i, v := range vals {
			if v == nil {
				row[i] = "NULL"
			} else {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		rowsData = append(rowsData, row)
	}

	elapsed := time.Since(start).Seconds()

	s.recordQueryHistory(r.Context(), serverID, dbName, tabID, query, int64(elapsed*1000), int64(total))

	totalPages := (total + limit - 1) / limit
	if totalPages < 1 {
		totalPages = 1
	}

	RenderPartial(w, "query_result.html", map[string]any{
		"Headers":    headers,
		"Rows":       rowsData,
		"Page":       page,
		"Total":      total,
		"TotalPages": totalPages,
		"Limit":      limit,
		"Offset":     offset,
		"Elapsed":    elapsed,
		"Message":    "",
	})
}

// isRowReturning reports whether the trimmed query is one that returns a
// result set (and can therefore be wrapped for count/pagination). All other
// statements (DDL/DML) are executed directly by handleExecuteQuery.
func isRowReturning(query string) bool {
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	if trimmed == "" {
		return false
	}
	// Skip leading comment lines.
	for strings.HasPrefix(trimmed, "--") {
		if idx := strings.Index(trimmed, "\n"); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[idx+1:])
		} else {
			return false
		}
	}
	// Advance past an optional leading "WITH x AS (...) " CTE to the final
	// statement keyword.
	idx := strings.Index(trimmed, " ")
	if idx < 0 {
		idx = len(trimmed)
	}
	first := trimmed[:idx]
	switch first {
	case "SELECT", "VALUES", "TABLE", "SHOW", "EXPLAIN":
		return true
	}
	// A query that starts with WITH needs inspection: a trailing SELECT
	// returns rows, while a trailing INSERT/UPDATE/DELETE does not.
	if first == "WITH" {
		return strings.HasSuffix(trimmed, "SELECT") || strings.HasSuffix(trimmed, ")")
	}
	return false
}

func (s *Server) handleTableColumns(w http.ResponseWriter, r *http.Request) {
	pool, _, _, schemaName, tableName, ok := s.loadTablePool(w, r)
	if !ok {
		return
	}

	items, err := pgdb.New(pool).GetTableColumns(r.Context(), pgdb.GetTableColumnsParams{
		TableSchema: schemaName,
		TableName:   tableName,
	})
	if err != nil {
		http.Error(w, "Failed to query columns", http.StatusInternalServerError)
		return
	}

	var cols []string
	for _, it := range items {
		cols = append(cols, getString(it))
	}

	query := "SELECT " + strings.Join(cols, ", ") + "\nFROM " + tableName + ";"

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"query": query})
}
