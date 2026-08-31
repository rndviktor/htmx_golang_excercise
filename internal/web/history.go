package web

import (
	"context"
	"database/sql"
	"encoding/base64"
	"log"
	"net/http"
	"strconv"
	"strings"

	sqlite "htmx-golang-excercise/internal/sqlc/sqlite/db"
)

// historyItemView is the display-friendly form of a query_history row passed
// to the template. QueryB64 holds the raw SQL base64-encoded so it can be
// stored safely in a data attribute and decoded on click.
type historyItemView struct {
	QueryB64   string
	QueryShort string
	Executed   string
	DurationMs sql.NullInt64
	Status     sql.NullString
}

// handleQueryHistory renders the query history panel for one script tab.
// The tab is identified by tab_id, and the entries are scoped to that tab's
// connection via server_id + db_name. When tab_id is absent the panel shows
// entries for the connection regardless of tab.
func (s *Server) handleQueryHistory(w http.ResponseWriter, r *http.Request) {
	serverID, _ := strconv.ParseInt(r.FormValue("server_id"), 10, 64)
	dbName := r.FormValue("db_name")
	tabID := r.FormValue("tab_id")

	connString := ""
	if serverID > 0 && dbName != "" {
		connString = encodeConnection(serverID, dbName).String
	}

	items, err := s.DB.ListQueryHistory(r.Context(), sqlite.ListQueryHistoryParams{
		UserID:       workspaceUserID(),
		Column2:      tabID,
		TabID:        sql.NullString{String: tabID, Valid: tabID != ""},
		Column4:      connString,
		ConnectionID: sql.NullString{String: connString, Valid: connString != ""},
	})
	if err != nil {
		log.Printf("Failed to load query history: %v", err)
		http.Error(w, "Failed to load query history", http.StatusInternalServerError)
		return
	}

	views := make([]historyItemView, 0, len(items))
	for _, it := range items {
		trimmed := strings.TrimSpace(it.QueryText)
		display := trimmed
		if len(display) > 120 {
			display = display[:120] + "…"
		}
		views = append(views, historyItemView{
			QueryB64:   base64.RawStdEncoding.EncodeToString([]byte(it.QueryText)),
			QueryShort: display,
			Executed:   formatHistoryTime(it.ExecutedAt),
			DurationMs: it.DurationMs,
			Status:     it.Status,
		})
	}

	RenderPartial(w, "query_history.html", map[string]any{
		"Items": views,
	})
}

func formatHistoryTime(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format("Jan 02 15:04:05")
}

// recordQueryHistory persists a completed query run to the SQLite
// query_history table so it shows up in the script window's history panel.
// tabID ties the entry to the specific script tab it was run from, keeping
// each tab's history independent.
func (s *Server) recordQueryHistory(ctx context.Context, serverID int64, dbName, tabID, query string, durationMs int64) {
	if serverID <= 0 || dbName == "" || tabID == "" || query == "" {
		return
	}

	conn := sql.NullString{String: "", Valid: false}
	if c := encodeConnection(serverID, dbName); c.Valid {
		conn = c
	}

	err := s.DB.RecordQueryHistory(ctx, sqlite.RecordQueryHistoryParams{
		UserID:       workspaceUserID(),
		TabID:        sql.NullString{String: tabID, Valid: true},
		ConnectionID: conn,
		QueryText:    query,
		DurationMs:   sql.NullInt64{Int64: durationMs, Valid: true},
		Status:       sql.NullString{String: "success", Valid: true},
	})
	if err != nil {
		log.Printf("Failed to record query history: %v", err)
	}
}
