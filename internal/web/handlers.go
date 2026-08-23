package web

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

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
		r.Get("/api/servers/new", s.handleNewServerModal)
		r.Post("/api/servers", s.handleAddServer)
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
	servers, err := s.DB.ListServersByGroup(r.Context(), db.DefaultUserID)
	if err != nil {
		log.Printf("Failed to list servers: %v", err)
		http.Error(w, "Failed to load servers", http.StatusInternalServerError)
		return
	}

	// Render partial fragment directly
	RenderPartial(w, "tree_node.html", map[string]any{
		"Servers": servers,
	})
}

func (s *Server) handleNewServerModal(w http.ResponseWriter, r *http.Request) {
	RenderPartial(w, "add_server_modal.html", nil)
}
