package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"htmx-golang-excercise/internal/db"
	sqlc "htmx-golang-excercise/internal/sqlc/db"
)

type ServerConfig struct {
	ID       string
	Name     string
	Host     string
	Port     int
	DBName   string
	Username string
	Password string
}

func (c ServerConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		c.Username, c.Password, c.Host, c.Port, c.DBName)
}

// Global/Server-level pool storage map
var (
	serverPools   = make(map[string]*pgxpool.Pool)
	serverConfigs = make(map[string]ServerConfig)
	mu            sync.RWMutex
)

func (s *Server) handleAddServer(w http.ResponseWriter, r *http.Request) {
	// Chi handlers use the standard http.HandlerFunc signature; the chi route
	// context lives inside r.Context(). URL params are read via
	// chi.URLParam(r, "name") once a route declares them, e.g. /api/servers/{serverID}.

	// 1. Parse Form
	port, _ := strconv.Atoi(r.FormValue("port"))
	if port == 0 {
		port = 5432
	}

	cfg := ServerConfig{
		ID:       fmt.Sprintf("server-%d", time.Now().UnixNano()),
		Name:     r.FormValue("name"),
		Host:     r.FormValue("host"),
		Port:     port,
		DBName:   r.FormValue("dbname"),
		Username: r.FormValue("username"),
		Password: r.FormValue("password"),
	}

	// 2. Test PostgreSQL Connection with pgxpool
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DSN())
	if err != nil {
		s.renderModalError(w, fmt.Sprintf("Configuration error: %v", err))
		return
	}

	// Ping database to verify credentials & connectivity
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		s.renderModalError(w, fmt.Sprintf("Connection failed: %v", err))
		return
	}

	// 3. Save Connection & Pool
	mu.Lock()
	serverConfigs[cfg.ID] = cfg
	serverPools[cfg.ID] = pool
	mu.Unlock()

	// 4. Persist verified server to SQLite
	if _, err := s.DB.CreateServer(ctx, sqlc.CreateServerParams{
		UserID:        db.DefaultUserID,
		ServergroupID: db.DefaultServerGroupID,
		Name:          cfg.Name,
		Host:          cfg.Host,
		Port:          int64(cfg.Port),
		MaintenanceDb: cfg.DBName,
		Username:      cfg.Username,
	}); err != nil {
		mu.Lock()
		delete(serverConfigs, cfg.ID)
		delete(serverPools, cfg.ID)
		mu.Unlock()
		pool.Close()
		s.renderModalError(w, fmt.Sprintf("Failed to store server: %v", err))
		return
	}

	// 5. Success: the empty 204 body swaps into #modal-container, which closes
	//    the modal. The HX-Trigger event makes #tree-root re-fetch /api/tree,
	//    re-rendering the sidebar with the newly added server.
	w.Header().Set("HX-Trigger", "server-added")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) renderModalError(w http.ResponseWriter, errorMsg string) {
	w.WriteHeader(http.StatusBadRequest)
	RenderPartial(w, "add_server_modal.html", map[string]any{
		"Error": errorMsg,
	})
}
