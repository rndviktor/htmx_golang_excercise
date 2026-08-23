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
	pool, _, ok := s.loadServerPool(w, r)
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

	RenderPartial(w, "tree_databases.html", map[string]any{"Databases": names})
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
