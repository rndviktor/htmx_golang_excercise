package web

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
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
	SslMode  string
}

func (c ServerConfig) DSN() string {
	sslMode := c.SslMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.Username, c.Password, c.Host, c.Port, c.DBName, sslMode)
}

// validSslModes are the libpq sslmode values accepted from the register form.
var validSslModes = map[string]bool{"disable": true, "prefer": true, "require": true}

// Global/Server-level pool storage map
var (
	serverPools = make(map[string]*pgxpool.Pool)
	// serverConfigs holds in-memory configs registered during this session.
	serverConfigs = make(map[string]ServerConfig)
	// dbPools caches pools for the maintenance DB, keyed by SQLite server row id.
	dbPools = make(map[int64]*pgxpool.Pool)
	// dbSpecificPools caches pools connected to an individual database of a
	// server (database-level tree nodes must query that database's catalogs,
	// not the server's maintenance DB).
	dbSpecificPools = make(map[dbPoolKey]*pgxpool.Pool)
	mu              sync.RWMutex
)

type dbPoolKey struct {
	ServerID int64
	Database string
}

// getOrCreatePool returns a cached pgx pool for the SQLite server row id,
// creating it on first use from the stored connection settings. Rows created
// before passwords were persisted have a NULL password; for those, fall back
// to a password captured in-memory when the server was registered.
func (s *Server) getOrCreatePool(ctx context.Context, id int64) (*pgxpool.Pool, error) {
	mu.RLock()
	pool, ok := dbPools[id]
	mu.RUnlock()
	if ok {
		return pool, nil
	}

	srv, err := s.DB.GetServerByID(ctx, sqlc.GetServerByIDParams{ID: id, UserID: db.DefaultUserID})
	if err != nil {
		return nil, fmt.Errorf("server %d not found", id)
	}

	pool, err = dialServer(ctx, srv, srv.MaintenanceDb)
	if err != nil {
		return nil, err
	}

	mu.Lock()
	dbPools[id] = pool
	mu.Unlock()
	log.Printf("Connected to server %d (%s:%d/%s)", id, srv.Host, srv.Port, srv.MaintenanceDb)

	return pool, nil
}

// getOrCreateDbPool returns a cached pgx pool connected to one specific
// database of a registered server. Used by database-level tree endpoints,
// whose catalog queries must run against that database itself.
func (s *Server) getOrCreateDbPool(ctx context.Context, id int64, database string) (*pgxpool.Pool, error) {
	key := dbPoolKey{ServerID: id, Database: database}
	mu.RLock()
	pool, ok := dbSpecificPools[key]
	mu.RUnlock()
	if ok {
		return pool, nil
	}

	srv, err := s.DB.GetServerByID(ctx, sqlc.GetServerByIDParams{ID: id, UserID: db.DefaultUserID})
	if err != nil {
		return nil, fmt.Errorf("server %d not found", id)
	}

	pool, err = dialServer(ctx, srv, database)
	if err != nil {
		return nil, err
	}

	mu.Lock()
	dbSpecificPools[key] = pool
	mu.Unlock()
	log.Printf("Connected to server %d database %q", id, database)

	return pool, nil
}

// dialServer opens and pings a new pgx pool to dbname on the given registered
// server, reusing the stored credentials (or the in-memory fallback for rows
// created before passwords were persisted).
func dialServer(ctx context.Context, srv sqlc.Server, dbname string) (*pgxpool.Pool, error) {
	password := ""
	if srv.Password.Valid {
		password = srv.Password.String
	} else {
		password = cachedPassword(srv.Name, srv.Host, srv.Port)
	}

	sslMode := srv.SslMode
	if sslMode == "" {
		sslMode = "prefer"
	}

	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(srv.Username, password),
		Host:   net.JoinHostPort(srv.Host, strconv.FormatInt(srv.Port, 10)),
		Path:   "/" + dbname,
	}
	q := u.Query()
	q.Set("sslmode", sslMode)
	q.Set("connect_timeout", "5")
	u.RawQuery = q.Encode()

	connectCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	pool, err := pgxpool.New(connectCtx, u.String())
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		if password == "" {
			return nil, fmt.Errorf(
				"connection failed and no password is stored for this server "+
					"(remove it and register again to save credentials): %w", err)
		}
		return nil, fmt.Errorf("connection failed: %w", err)
	}

	return pool, nil
}

// cachedPassword looks up a password from the in-memory configs registered
// during this session, matching on name/host/port.
func cachedPassword(name, host string, port int64) string {
	mu.RLock()
	defer mu.RUnlock()
	for _, cfg := range serverConfigs {
		if cfg.Name == name && cfg.Host == host && int64(cfg.Port) == port {
			return cfg.Password
		}
	}
	return ""
}

func (s *Server) handleAddServer(w http.ResponseWriter, r *http.Request) {
	// Chi handlers use the standard http.HandlerFunc signature; the chi route
	// context lives inside r.Context(). URL params are read via
	// chi.URLParam(r, "name") once a route declares them, e.g. /api/servers/{serverID}.

	// 1. Parse Form
	port, _ := strconv.Atoi(r.FormValue("port"))
	if port == 0 {
		port = 5432
	}

	sslMode := r.FormValue("sslmode")
	if !validSslModes[sslMode] {
		sslMode = "disable"
	}

	cfg := ServerConfig{
		ID:       fmt.Sprintf("server-%d", time.Now().UnixNano()),
		Name:     r.FormValue("name"),
		Host:     r.FormValue("host"),
		Port:     port,
		DBName:   r.FormValue("dbname"),
		Username: r.FormValue("username"),
		Password: r.FormValue("password"),
		SslMode:  sslMode,
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
	created, err := s.DB.CreateServer(ctx, sqlc.CreateServerParams{
		UserID:        db.DefaultUserID,
		ServergroupID: db.DefaultServerGroupID,
		Name:          cfg.Name,
		Host:          cfg.Host,
		Port:          int64(cfg.Port),
		MaintenanceDb: cfg.DBName,
		Username:      cfg.Username,
		SslMode:       cfg.SslMode,
	})
	if err != nil {
		mu.Lock()
		delete(serverConfigs, cfg.ID)
		delete(serverPools, cfg.ID)
		mu.Unlock()
		pool.Close()
		s.renderModalError(w, fmt.Sprintf("Failed to store server: %v", err))
		return
	}

	// 4b. Persist the password too (the sqlc-generated CreateServer does not
	//     cover it). Without it the tree browser cannot reconnect after a
	//     restart and fails SASL auth with an empty password.
	if _, err := s.sqliteDB.ExecContext(ctx,
		`UPDATE server SET password = ? WHERE id = ?`, cfg.Password, created.ID); err != nil {
		log.Printf("Failed to persist password for server %d: %v", created.ID, err)
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
