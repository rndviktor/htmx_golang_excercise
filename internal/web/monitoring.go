package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// monitoringResponse is a point-in-time snapshot of PostgreSQL health metrics
// for a single database. The frontend polls this endpoint periodically and
// accumulates the deltas into live time-series charts.
type monitoringResponse struct {
	ActiveConnections int64         `json:"activeConnections"`
	MaxConnections    int64         `json:"maxConnections"`
	DatabaseSizeBytes int64         `json:"dbSizeBytes"`

	ActiveTxCount int64 `json:"activeTx"`
	IdleTxCount   int64 `json:"idleTx"`
	IdleCount     int64 `json:"idle"`
	BlockedQueries int64 `json:"blockedQueries"`

	BlksHit      int64 `json:"blksHit"`
	BlksRead     int64 `json:"blksRead"`

	XactCommit   int64 `json:"xactCommit"`
	XactRollback int64 `json:"xactRollback"`

	ReplicationLagBytes int64 `json:"replicationLagBytes"`
	HasReplication      bool  `json:"hasReplication"`

	Inserts int64 `json:"inserts"`
	Updates int64 `json:"updates"`
	Deletes int64 `json:"deletes"`
}

// handleMonitoring returns the current health snapshot for the database
// identified by {serverID}/{dbName}. It is called by the dashboard to render
// the live monitoring KPIs and time-series graphs.
func (s *Server) handleMonitoring(w http.ResponseWriter, r *http.Request) {
	pool, _, _, ok := s.loadDatabasePool(w, r)
	if !ok {
		return
	}

	resp, err := queryMonitoring(r, pool)
	if err != nil {
		log.Printf("[monitoring] %s: %v", r.URL.Path, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// queryMonitoring runs the catalog queries that produce a monitoring snapshot.
func queryMonitoring(r *http.Request, pool *pgxpool.Pool) (*monitoringResponse, error) {
	ctx := r.Context()
	resp := &monitoringResponse{}

	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity").
		Scan(&resp.ActiveConnections); err != nil {
		return nil, err
	}

	var maxConn string
	if qerr := pool.QueryRow(ctx, "SHOW max_connections").Scan(&maxConn); qerr != nil {
		return nil, qerr
	}
	parsed, perr := strconv.ParseInt(strings.TrimSpace(maxConn), 10, 64)
	if perr != nil {
		return nil, fmt.Errorf("parse max_connections %q: %w", maxConn, perr)
	}
	resp.MaxConnections = parsed

	if err := pool.QueryRow(ctx, "SELECT pg_database_size(current_database())").
		Scan(&resp.DatabaseSizeBytes); err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx, `
		SELECT COALESCE(state, 'unknown') AS state, count(*)
		FROM pg_stat_activity
		WHERE state <> 'active' OR state IS NULL
		GROUP BY state`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var state string
		var cnt int64
		if err := rows.Scan(&state, &cnt); err != nil {
			rows.Close()
			return nil, err
		}
		switch state {
		case "idle":
			resp.IdleCount += cnt
		case "idle in transaction":
			resp.IdleTxCount += cnt
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Count active sessions separately (excluding the monitoring connection's
	// own queries which are not active at snapshot time beyond the count query).
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_stat_activity WHERE state = 'active'`).
		Scan(&resp.ActiveTxCount); err != nil {
		return nil, err
	}

	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock'`).
		Scan(&resp.BlockedQueries); err != nil {
		return nil, err
	}

	if err := pool.QueryRow(ctx, `
		SELECT blks_hit, blks_read
		FROM pg_stat_database WHERE datname = current_database()`).
		Scan(&resp.BlksHit, &resp.BlksRead); err != nil {
		return nil, err
	}

	if err := pool.QueryRow(ctx, `
		SELECT xact_commit, xact_rollback
		FROM pg_stat_database WHERE datname = current_database()`).
		Scan(&resp.XactCommit, &resp.XactRollback); err != nil {
		return nil, err
	}

	// Replication lag in bytes (WAL bytes sent but not yet replayed). When
	// there are no replicas, the query returns no rows and HasReplication
	// stays false.
	var lagBytes *int64
	err = pool.QueryRow(ctx, `
		SELECT pg_wal_lsn_diff(sent_lsn, replay_lsn)::bigint
		FROM pg_stat_replication`).Scan(&lagBytes)
	if err == nil {
		resp.HasReplication = true
		if lagBytes != nil {
			resp.ReplicationLagBytes = *lagBytes
		}
	}

	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(sum(n_tup_ins),0), COALESCE(sum(n_tup_upd),0), COALESCE(sum(n_tup_del),0)
		FROM pg_stat_user_tables`).
		Scan(&resp.Inserts, &resp.Updates, &resp.Deletes); err != nil {
		return nil, err
	}

	return resp, nil
}
