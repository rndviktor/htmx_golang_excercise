package web

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"htmx-golang-excercise/internal/db"
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
		r.Get("/api/servers", s.handleServerList)
		r.Get("/api/servers/new", s.handleNewServerModal)
		r.Get("/api/tabs/script-panel", s.handleScriptTabPanel)
		r.Post("/api/execute-query", s.handleExecuteQuery)
		r.Get("/api/servers/{serverID}/databases/{dbName}/schemas/{schemaName}/tables/{tableName}/columns", s.handleTableColumns)
		r.Post("/api/servers", s.handleAddServer)
		r.Get("/api/servers/{serverID}/children", s.handleServerChildren)
		r.Get("/api/servers/{serverID}/databases", s.handleServerDatabases)
		r.Get("/api/servers/{serverID}/databases/{dbName}/children", s.handleDatabaseChildren)
		r.Get("/api/servers/{serverID}/databases/{dbName}/{category}", s.handleDatabaseCategory)
		r.Get("/api/servers/{serverID}/databases/{dbName}/schemas/{schemaName}/children", s.handleSchemaChildren)
		r.Get("/api/servers/{serverID}/databases/{dbName}/schemas/{schemaName}/{category}", s.handleSchemaCategory)
		r.Get("/api/servers/{serverID}/databases/{dbName}/schemas/{schemaName}/tables/{tableName}/children", s.handleTableChildren)
		r.Get("/api/servers/{serverID}/databases/{dbName}/schemas/{schemaName}/tables/{tableName}/{category}", s.handleTableCategory)
		r.Get("/api/servers/{serverID}/roles", s.handleServerRoles)
		r.Get("/api/servers/{serverID}/tablespaces", s.handleServerTablespaces)
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

	names, ok := s.queryNames(w, r, pool, "databases",
		`SELECT datname FROM pg_database WHERE datallowconn AND NOT datistemplate ORDER BY datname`)
	if !ok {
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

	names, ok := s.queryNames(w, r, pool, cat.Label, cat.Query)
	if !ok {
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

	names, ok := s.queryNames(w, r, pool, cat.Label, cat.Query, schemaName)
	if !ok {
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

	pool, _, _, _, _, ok := s.loadTablePool(w, r)
	if !ok {
		return
	}

	names, ok := s.queryNames(w, r, pool, cat.Label, cat.Query, chi.URLParam(r, "schemaName"), chi.URLParam(r, "tableName"))
	if !ok {
		return
	}

	renderTree(w, leaves(cat.Icon, names), cat.Empty)
}

func (s *Server) handleServerRoles(w http.ResponseWriter, r *http.Request) {
	pool, _, ok := s.loadServerPool(w, r)
	if !ok {
		return
	}

	names, ok := s.queryNames(w, r, pool, "roles", `SELECT rolname FROM pg_roles ORDER BY rolname`)
	if !ok {
		return
	}

	renderTree(w, leaves("👤", names), "No roles found.")
}

func (s *Server) handleServerTablespaces(w http.ResponseWriter, r *http.Request) {
	pool, _, ok := s.loadServerPool(w, r)
	if !ok {
		return
	}

	names, ok := s.queryNames(w, r, pool, "tablespaces", `SELECT spcname FROM pg_tablespace ORDER BY spcname`)
	if !ok {
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

	// Count total rows
	var total int
	err = pool.QueryRow(r.Context(),
		"SELECT COUNT(*) FROM ("+query+") _cnt").Scan(&total)
	if err != nil {
		http.Error(w, "Count query failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Fetch paginated rows
	rows, err := pool.Query(r.Context(),
		"SELECT * FROM ("+query+") _q LIMIT $1 OFFSET $2", limit, offset)
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
	})
}

func (s *Server) handleTableColumns(w http.ResponseWriter, r *http.Request) {
	pool, _, _, schemaName, tableName, ok := s.loadTablePool(w, r)
	if !ok {
		return
	}

	rows, err := pool.Query(r.Context(),
		`SELECT column_name FROM information_schema.columns
		 WHERE table_schema = $1 AND table_name = $2
		 ORDER BY ordinal_position`, schemaName, tableName)
	if err != nil {
		http.Error(w, "Failed to query columns", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			http.Error(w, "Failed to scan column", http.StatusInternalServerError)
			return
		}
		cols = append(cols, name)
	}

	query := "SELECT " + strings.Join(cols, ", ") + "\nFROM " + tableName + ";"

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"query": query})
}
