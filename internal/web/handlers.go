package web

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"htmx-golang-excercise/internal/db"
	sqlc "htmx-golang-excercise/internal/sqlc/db"
)

type Server struct {
	DB       *sqlc.Queries
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
		r.Post("/api/servers", s.handleAddServer)
		r.Get("/api/servers/{serverID}/children", s.handleServerChildren)
		r.Get("/api/servers/{serverID}/databases", s.handleServerDatabases)
		r.Get("/api/servers/{serverID}/databases/{dbName}/children", s.handleDatabaseChildren)
		r.Get("/api/servers/{serverID}/databases/{dbName}/{category}", s.handleDatabaseCategory)
		r.Get("/api/servers/{serverID}/databases/{dbName}/schemas/{schemaName}/children", s.handleSchemaChildren)
		r.Get("/api/servers/{serverID}/databases/{dbName}/schemas/{schemaName}/{category}", s.handleSchemaCategory)
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

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	// Root node of the object explorer: the "Servers" server group, labeled
	// with the number of registered servers. Its children are lazy-loaded
	// on click via GET /api/servers.
	servers, err := s.DB.ListServersByGroup(r.Context(), db.DefaultUserID)
	if err != nil {
		log.Printf("Failed to list servers: %v", err)
		http.Error(w, "Failed to load servers", http.StatusInternalServerError)
		return
	}

	RenderPartial(w, "tree_node.html", map[string]any{
		"ServerCount": len(servers),
	})
}

func (s *Server) handleServerList(w http.ResponseWriter, r *http.Request) {
	servers, err := s.DB.ListServersByGroup(r.Context(), db.DefaultUserID)
	if err != nil {
		log.Printf("Failed to list servers: %v", err)
		http.Error(w, "Failed to load servers", http.StatusInternalServerError)
		return
	}

	RenderPartial(w, "tree_servers.html", map[string]any{
		"Servers": servers,
	})
}

func (s *Server) handleServerChildren(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "serverID"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "Invalid server id", http.StatusBadRequest)
		return
	}

	if _, err := s.DB.GetServerByID(r.Context(), sqlc.GetServerByIDParams{
		ID:     id,
		UserID: db.DefaultUserID,
	}); err != nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	RenderPartial(w, "tree_server_children.html", map[string]any{
		"ServerID": id,
	})
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

func (s *Server) handleServerDatabases(w http.ResponseWriter, r *http.Request) {
	pool, id, ok := s.loadServerPool(w, r)
	if !ok {
		return
	}

	rows, err := pool.Query(r.Context(),
		`SELECT datname FROM pg_database WHERE datallowconn AND NOT datistemplate ORDER BY datname`)
	if err != nil {
		log.Printf("Failed to query pg_database: %v", err)
		http.Error(w, "Failed to load databases", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			log.Printf("Failed to scan database name: %v", err)
			http.Error(w, "Failed to load databases", http.StatusInternalServerError)
			return
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Failed to iterate pg_database: %v", err)
		http.Error(w, "Failed to load databases", http.StatusInternalServerError)
		return
	}

	RenderPartial(w, "tree_databases.html", map[string]any{
		"Databases": names,
		"ServerID":  id,
	})
}

// dbCategory describes one expandable folder under a database node. The
// Query runs against the selected database itself (not the maintenance DB),
// so it needs the per-database pool from loadDatabasePool.
type dbCategory struct {
	Slug  string
	Label string
	Query string
	Icon  string
	Empty string
}

var dbCategories = []dbCategory{
	{Slug: "casts", Label: "Casts", Icon: "🔁", Empty: "No casts found.",
		Query: `SELECT '(' || castsource::regtype || ' AS ' || casttarget::regtype || ')' FROM pg_cast ORDER BY 1`},
	{Slug: "catalogs", Label: "Catalogs", Icon: "📚", Empty: "No catalogs found.",
		Query: `SELECT nspname FROM pg_namespace WHERE nspname = 'information_schema' OR nspname LIKE 'pg\_%' ORDER BY 1`},
	{Slug: "event-triggers", Label: "Event Triggers", Icon: "⚡", Empty: "No event triggers found.",
		Query: `SELECT evtname FROM pg_event_trigger ORDER BY 1`},
	{Slug: "extensions", Label: "Extensions", Icon: "🧩", Empty: "No extensions found.",
		Query: `SELECT extname FROM pg_extension ORDER BY 1`},
	{Slug: "foreign-data-wrappers", Label: "Foreign Data Wrappers", Icon: "🌐", Empty: "No foreign data wrappers found.",
		Query: `SELECT fdwname FROM pg_foreign_data_wrapper ORDER BY 1`},
	{Slug: "languages", Label: "Languages", Icon: "🗣️", Empty: "No languages found.",
		Query: `SELECT lanname FROM pg_language WHERE lanispl ORDER BY 1`},
	{Slug: "publications", Label: "Publications", Icon: "📢", Empty: "No publications found.",
		Query: `SELECT pubname FROM pg_publication ORDER BY 1`},
	{Slug: "schemas", Label: "Schemas", Icon: "📁", Empty: "No schemas found.",
		Query: `SELECT nspname FROM pg_namespace WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema' ORDER BY 1`},
	{Slug: "subscriptions", Label: "Subscriptions", Icon: "📥", Empty: "No subscriptions found.",
		Query: `SELECT subname FROM pg_subscription WHERE subdbid = (SELECT oid FROM pg_database WHERE datname = current_database()) ORDER BY 1`},
}

// handleDatabaseChildren renders the category folders shown when a database
// node is expanded in the tree.
func (s *Server) handleDatabaseChildren(w http.ResponseWriter, r *http.Request) {
	_, id, dbName, ok := s.loadDatabasePool(w, r)
	if !ok {
		return
	}

	RenderPartial(w, "tree_database_children.html", map[string]any{
		"ServerID":   id,
		"Database":   dbName,
		"Categories": dbCategories,
	})
}

// handleDatabaseCategory lists the contents of one database category folder
// (casts, extensions, schemas, ...) queried live from that database.
func (s *Server) handleDatabaseCategory(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "category")
	var cat *dbCategory
	for i := range dbCategories {
		if dbCategories[i].Slug == slug {
			cat = &dbCategories[i]
			break
		}
	}
	if cat == nil {
		http.Error(w, "Unknown database item category", http.StatusNotFound)
		return
	}

	pool, id, dbName, ok := s.loadDatabasePool(w, r)
	if !ok {
		return
	}

	rows, err := pool.Query(r.Context(), cat.Query)
	if err != nil {
		log.Printf("Failed to query %s in database tree: %v", slug, err)
		http.Error(w, "Failed to load "+cat.Label, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			log.Printf("Failed to scan %s name: %v", slug, err)
			http.Error(w, "Failed to load "+cat.Label, http.StatusInternalServerError)
			return
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Failed to iterate %s: %v", slug, err)
		http.Error(w, "Failed to load "+cat.Label, http.StatusInternalServerError)
		return
	}

	// Schemas are not leaves: each one is an expandable node whose children
	// are the schema's own object folders (tables, views, ...).
	if slug == "schemas" {
		RenderPartial(w, "tree_schemas.html", map[string]any{
			"ServerID": id,
			"Database": dbName,
			"Schemas":  names,
		})
		return
	}

	RenderPartial(w, "tree_db_items.html", map[string]any{
		"Icon":         cat.Icon,
		"Items":        names,
		"EmptyMessage": cat.Empty,
	})
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

// schemaCategory describes one expandable folder inside a schema node. The
// Query takes the schema name as $1 and runs against the connected database.
type schemaCategory struct {
	Slug  string
	Label string
	Query string
	Icon  string
	Empty string
}

var schemaCategories = []schemaCategory{
	{Slug: "tables", Label: "Tables", Icon: "📋", Empty: "No tables found.",
		Query: `SELECT tablename FROM pg_tables WHERE schemaname = $1 ORDER BY 1`},
	{Slug: "views", Label: "Views", Icon: "🔭", Empty: "No views found.",
		Query: `SELECT viewname FROM pg_views WHERE schemaname = $1 ORDER BY 1`},
	{Slug: "materialized-views", Label: "Materialized Views", Icon: "🧊", Empty: "No materialized views found.",
		Query: `SELECT matviewname FROM pg_matviews WHERE schemaname = $1 ORDER BY 1`},
	{Slug: "sequences", Label: "Sequences", Icon: "🔢", Empty: "No sequences found.",
		Query: `SELECT sequencename FROM pg_sequences WHERE schemaname = $1 ORDER BY 1`},
	{Slug: "functions", Label: "Functions", Icon: "⚙️", Empty: "No functions found.",
		Query: `SELECT p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')' ` +
			`FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid ` +
			`WHERE n.nspname = $1 AND p.prokind = 'f' ORDER BY 1`},
	{Slug: "procedures", Label: "Procedures", Icon: "🛠️", Empty: "No procedures found.",
		Query: `SELECT p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')' ` +
			`FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid ` +
			`WHERE n.nspname = $1 AND p.prokind = 'p' ORDER BY 1`},
	{Slug: "types", Label: "Types", Icon: "🏷️", Empty: "No types found.",
		Query: `SELECT t.typname FROM pg_type t JOIN pg_namespace n ON t.typnamespace = n.oid ` +
			`WHERE n.nspname = $1 AND t.typtype IN ('c', 'e', 'r') ORDER BY 1`},
	{Slug: "domains", Label: "Domains", Icon: "📐", Empty: "No domains found.",
		Query: `SELECT t.typname FROM pg_type t JOIN pg_namespace n ON t.typnamespace = n.oid ` +
			`WHERE n.nspname = $1 AND t.typtype = 'd' ORDER BY 1`},
}

// handleSchemaChildren renders the object folders shown when a schema node is
// expanded in the tree.
func (s *Server) handleSchemaChildren(w http.ResponseWriter, r *http.Request) {
	_, id, dbName, schemaName, ok := s.loadSchemaPool(w, r)
	if !ok {
		return
	}

	RenderPartial(w, "tree_schema_children.html", map[string]any{
		"ServerID":   id,
		"Database":   dbName,
		"Schema":     schemaName,
		"Categories": schemaCategories,
	})
}

// handleSchemaCategory lists the contents of one folder inside a schema
// (tables, views, sequences, ...) queried live from that database.
func (s *Server) handleSchemaCategory(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "category")
	var cat *schemaCategory
	for i := range schemaCategories {
		if schemaCategories[i].Slug == slug {
			cat = &schemaCategories[i]
			break
		}
	}
	if cat == nil {
		http.Error(w, "Unknown schema item category", http.StatusNotFound)
		return
	}

	pool, _, _, schemaName, ok := s.loadSchemaPool(w, r)
	if !ok {
		return
	}

	rows, err := pool.Query(r.Context(), cat.Query, schemaName)
	if err != nil {
		log.Printf("Failed to query %s in schema %q: %v", slug, schemaName, err)
		http.Error(w, "Failed to load "+cat.Label, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			log.Printf("Failed to scan %s name: %v", slug, err)
			http.Error(w, "Failed to load "+cat.Label, http.StatusInternalServerError)
			return
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Failed to iterate %s: %v", slug, err)
		http.Error(w, "Failed to load "+cat.Label, http.StatusInternalServerError)
		return
	}

	RenderPartial(w, "tree_db_items.html", map[string]any{
		"Icon":         cat.Icon,
		"Items":        names,
		"EmptyMessage": cat.Empty,
	})
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

func (s *Server) handleServerRoles(w http.ResponseWriter, r *http.Request) {
	pool, _, ok := s.loadServerPool(w, r)
	if !ok {
		return
	}

	rows, err := pool.Query(r.Context(),
		`SELECT rolname FROM pg_roles ORDER BY rolname`)
	if err != nil {
		log.Printf("Failed to query pg_roles: %v", err)
		http.Error(w, "Failed to load roles", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			log.Printf("Failed to scan role name: %v", err)
			http.Error(w, "Failed to load roles", http.StatusInternalServerError)
			return
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Failed to iterate pg_roles: %v", err)
		http.Error(w, "Failed to load roles", http.StatusInternalServerError)
		return
	}

	RenderPartial(w, "tree_roles.html", map[string]any{"Roles": names})
}

func (s *Server) handleServerTablespaces(w http.ResponseWriter, r *http.Request) {
	pool, _, ok := s.loadServerPool(w, r)
	if !ok {
		return
	}

	rows, err := pool.Query(r.Context(),
		`SELECT spcname FROM pg_tablespace ORDER BY spcname`)
	if err != nil {
		log.Printf("Failed to query pg_tablespace: %v", err)
		http.Error(w, "Failed to load tablespaces", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			log.Printf("Failed to scan tablespace name: %v", err)
			http.Error(w, "Failed to load tablespaces", http.StatusInternalServerError)
			return
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Failed to iterate pg_tablespace: %v", err)
		http.Error(w, "Failed to load tablespaces", http.StatusInternalServerError)
		return
	}

	RenderPartial(w, "tree_tablespaces.html", map[string]any{"Tablespaces": names})
}

func (s *Server) handleNewServerModal(w http.ResponseWriter, r *http.Request) {
	RenderPartial(w, "add_server_modal.html", nil)
}
