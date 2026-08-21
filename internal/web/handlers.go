package web

import "net/http"

type Server struct {
}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /login", s.handleLoginGet)
	mux.HandleFunc("POST /login", s.handleLoginPost)
	mux.HandleFunc("POST /logout", s.handleLogout)

	mux.HandleFunc("GET /", s.RequireAuth(s.handleIndex))
	mux.HandleFunc("GET /api/tree", s.RequireAuth(s.handleTree))

	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	Render(w, "index.html", map[string]any{
		"Title":         "The Main Dashboard",
		"Authenticated": true,
		"Username":      GetAuthenticatedUser(r),
	})
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"Schemas": []string{"public", "information_schema", "pg_catalog"},
	}

	// Render partial fragment directly
	RenderPartial(w, "tree_node.html", data)
}
