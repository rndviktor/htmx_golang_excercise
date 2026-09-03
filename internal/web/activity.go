package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// loadPoolFromQuery reads server_id and db_name from query parameters and
// returns a pgx pool for that database. Used by the sessions/locks/prepared-
// transactions endpoints which are called via fetch() with explicit params.
func (s *Server) loadPoolFromQuery(w http.ResponseWriter, r *http.Request) (*pgxpool.Pool, bool) {
	serverIDStr := r.URL.Query().Get("server_id")
	dbName := r.URL.Query().Get("db_name")
	if serverIDStr == "" || dbName == "" {
		http.Error(w, "Missing server_id or db_name", http.StatusBadRequest)
		return nil, false
	}
	serverID, err := strconv.ParseInt(serverIDStr, 10, 64)
	if err != nil || serverID < 1 {
		http.Error(w, "Invalid server_id", http.StatusBadRequest)
		return nil, false
	}
	pool, err := s.getOrCreateDbPool(r.Context(), serverID, dbName)
	if err != nil {
		http.Error(w, "Cannot connect to database: "+err.Error(), http.StatusBadGateway)
		return nil, false
	}
	return pool, true
}

type sessionRow struct {
	PID             interface{} `json:"pid"`
	Usename         interface{} `json:"usename"`
	ApplicationName interface{} `json:"application_name"`
	ClientAddr      interface{} `json:"client_addr"`
	BackendStart    interface{} `json:"backend_start"`
	XactStart       interface{} `json:"xact_start"`
	State           interface{} `json:"state"`
	WaitEventType   interface{} `json:"wait_event_type"`
	WaitEvent       interface{} `json:"wait_event"`
	BlockingPIDs    interface{} `json:"blocking_pids"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	log.Printf("[sessions] request: %s", r.URL.String())
	pool, ok := s.loadPoolFromQuery(w, r)
	if !ok {
		log.Printf("[sessions] loadPoolFromQuery failed")
		return
	}

	activeOnly := r.URL.Query().Get("active_only") == "true"
	search := strings.ToLower(r.URL.Query().Get("search"))

	query := `
		SELECT
			a.pid,
			a.usename,
			a.application_name,
			COALESCE(a.client_addr::text, '') AS client_addr,
			a.backend_start,
			a.xact_start,
			COALESCE(a.state, '') AS state,
			COALESCE(a.wait_event_type, '') AS wait_event_type,
			COALESCE(a.wait_event, '') AS wait_event,
			COALESCE((
				SELECT string_agg(DISTINCT blocker.pid::text, ', ')
				FROM pg_locks waiting
				JOIN pg_locks granted ON granted.locktype = waiting.locktype
					AND granted.relation = waiting.relation
					AND granted.page = waiting.page
					AND granted.tuple = waiting.tuple
					AND granted.granted
					AND granted.pid != waiting.pid
				JOIN pg_stat_activity blocker ON blocker.pid = granted.pid
				WHERE waiting.pid = a.pid AND NOT waiting.granted
			), '') AS blocking_pids
		FROM pg_stat_activity a
		WHERE a.pid != pg_backend_pid()
			AND (NOT $1 OR a.state = 'active')
		ORDER BY a.pid`

	rows, err := pool.Query(r.Context(), query, activeOnly)
	if err != nil {
		log.Printf("[sessions] query error: %v", err)
		http.Error(w, "Failed to query sessions: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var results []sessionRow
	for rows.Next() {
		var r sessionRow
		if err := rows.Scan(&r.PID, &r.Usename, &r.ApplicationName, &r.ClientAddr,
			&r.BackendStart, &r.XactStart, &r.State, &r.WaitEventType,
			&r.WaitEvent, &r.BlockingPIDs); err != nil {
			log.Printf("[sessions] scan error: %v", err)
			continue
		}
		// Client-side search filter
		if search != "" {
			fields := []string{
				fmt.Sprintf("%v", r.PID),
				fmt.Sprintf("%v", r.Usename),
				fmt.Sprintf("%v", r.ApplicationName),
				fmt.Sprintf("%v", r.ClientAddr),
				fmt.Sprintf("%v", r.State),
				fmt.Sprintf("%v", r.WaitEventType),
				fmt.Sprintf("%v", r.WaitEvent),
			}
			found := false
			for _, f := range fields {
				if strings.Contains(strings.ToLower(f), search) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		results = append(results, r)
	}

	log.Printf("[sessions] returning %d rows", len(results))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func (s *Server) handleSessionCancel(w http.ResponseWriter, r *http.Request) {
	pool, ok := s.loadPoolFromQuery(w, r)
	if !ok {
		return
	}

	pidStr := chi.URLParam(r, "pid")
	pid, err := strconv.ParseInt(pidStr, 10, 64)
	if err != nil || pid < 1 {
		http.Error(w, "Invalid PID", http.StatusBadRequest)
		return
	}

	_, err = pool.Exec(r.Context(), "SELECT pg_cancel_backend($1)", pid)
	if err != nil {
		http.Error(w, "Failed to cancel: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleSessionTerminate(w http.ResponseWriter, r *http.Request) {
	pool, ok := s.loadPoolFromQuery(w, r)
	if !ok {
		return
	}

	pidStr := chi.URLParam(r, "pid")
	pid, err := strconv.ParseInt(pidStr, 10, 64)
	if err != nil || pid < 1 {
		http.Error(w, "Invalid PID", http.StatusBadRequest)
		return
	}

	_, err = pool.Exec(r.Context(), "SELECT pg_terminate_backend($1)", pid)
	if err != nil {
		http.Error(w, "Failed to terminate: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type lockRow struct {
	PID                  interface{} `json:"pid"`
	Locktype             interface{} `json:"locktype"`
	Relation             interface{} `json:"relation"`
	Page                 interface{} `json:"page"`
	Tuple                interface{} `json:"tuple"`
	VirtualTransactionID interface{} `json:"virtual_transaction_id"`
	TransactionID        interface{} `json:"transaction_id"`
	ClassID              interface{} `json:"classid"`
	ObjID                interface{} `json:"objid"`
	VirtualXIDOwner      interface{} `json:"virtual_xid_owner"`
	Mode                 interface{} `json:"mode"`
	Granted              interface{} `json:"granted"`
}

func (s *Server) handleLocks(w http.ResponseWriter, r *http.Request) {
	log.Printf("[locks] request: %s", r.URL.String())
	pool, ok := s.loadPoolFromQuery(w, r)
	if !ok {
		log.Printf("[locks] loadPoolFromQuery failed")
		return
	}

	search := strings.ToLower(r.URL.Query().Get("search"))

	query := `
		SELECT
			l.pid,
			l.locktype,
			COALESCE(c.relname, '') AS relation,
			COALESCE(l.page::text, '') AS page,
			COALESCE(l.tuple::text, '') AS tuple,
			COALESCE(l.virtualxid, '') AS virtual_transaction_id,
			COALESCE(l.transactionid::text, '') AS transaction_id,
			COALESCE(l.classid::text, '') AS classid,
			COALESCE(l.objid::text, '') AS objid,
			COALESCE(l.virtualxid, '') AS virtual_xid_owner,
			l.mode,
			l.granted
		FROM pg_locks l
		LEFT JOIN pg_class c ON c.oid = l.relation
		ORDER BY l.pid`

	rows, err := pool.Query(r.Context(), query)
	if err != nil {
		http.Error(w, "Failed to query locks: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var results []lockRow
	for rows.Next() {
		var r lockRow
		if err := rows.Scan(&r.PID, &r.Locktype, &r.Relation, &r.Page, &r.Tuple,
			&r.VirtualTransactionID, &r.TransactionID, &r.ClassID, &r.ObjID,
			&r.VirtualXIDOwner, &r.Mode, &r.Granted); err != nil {
			continue
		}
		if search != "" {
			fields := []string{
				fmt.Sprintf("%v", r.PID),
				fmt.Sprintf("%v", r.Locktype),
				fmt.Sprintf("%v", r.Relation),
				fmt.Sprintf("%v", r.Mode),
				fmt.Sprintf("%v", r.Granted),
			}
			found := false
			for _, f := range fields {
				if strings.Contains(strings.ToLower(f), search) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		results = append(results, r)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

type preparedTxRow struct {
	Name       interface{} `json:"name"`
	Owner      interface{} `json:"owner"`
	XID        interface{} `json:"xid"`
	PreparedAt interface{} `json:"prepared_at"`
}

func (s *Server) handlePreparedTransactions(w http.ResponseWriter, r *http.Request) {
	log.Printf("[prepared-tx] request: %s", r.URL.String())
	pool, ok := s.loadPoolFromQuery(w, r)
	if !ok {
		log.Printf("[prepared-tx] loadPoolFromQuery failed")
		return
	}

	search := strings.ToLower(r.URL.Query().Get("search"))

	query := `
		SELECT
			COALESCE(gid, '') AS name,
			COALESCE(owner, '') AS owner,
			COALESCE(xid::text, '') AS xid,
			COALESCE(prepared::text, '') AS prepared_at
		FROM pg_prepared_xacts
		ORDER BY prepared`

	rows, err := pool.Query(r.Context(), query)
	if err != nil {
		// pg_prepared_xacts requires superuser or pg_monitor membership.
		// Return empty array on permission error rather than failing hard.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]preparedTxRow{})
		return
	}
	defer rows.Close()

	var results []preparedTxRow
	for rows.Next() {
		var r preparedTxRow
		if err := rows.Scan(&r.Name, &r.Owner, &r.XID, &r.PreparedAt); err != nil {
			continue
		}
		if search != "" {
			fields := []string{
				fmt.Sprintf("%v", r.Name),
				fmt.Sprintf("%v", r.Owner),
				fmt.Sprintf("%v", r.XID),
			}
			found := false
			for _, f := range fields {
				if strings.Contains(strings.ToLower(f), search) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		results = append(results, r)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}
