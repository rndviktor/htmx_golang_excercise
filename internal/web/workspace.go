package web

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"htmx-golang-excercise/internal/db"
	sqlite "htmx-golang-excercise/internal/sqlc/sqlite/db"
)

// workspaceTabJSON mirrors one open script tab of a user.
type workspaceTabJSON struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ServerID int64  `json:"server_id"`
	DBName   string `json:"db_name"`
	Query    string `json:"query"`
	TabOrder int    `json:"tab_order"`
}

// workspaceLayoutJSON holds the layout bits that survive a refresh.
type workspaceLayoutJSON struct {
	SidebarWidth string   `json:"sidebar_width"`
	SelectedTree string   `json:"selected_tree"`
	ExpandedTree []string `json:"expanded_tree"`
}

// workspaceStateJSON is the full persisted UI state of a user.
type workspaceStateJSON struct {
	ActiveTabID string              `json:"active_tab_id"`
	Layout      workspaceLayoutJSON `json:"layout"`
	Tabs        []workspaceTabJSON  `json:"tabs"`
}

func workspaceUserID() sql.NullInt64 {
	return sql.NullInt64{Int64: db.DefaultUserID, Valid: true}
}

// encodeConnection packs server id and database name into the single
// connection_id column of workspace_tabs.
func encodeConnection(serverID int64, dbName string) sql.NullString {
	if serverID <= 0 || dbName == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: strconv.FormatInt(serverID, 10) + ":" + dbName, Valid: true}
}

func decodeConnection(c sql.NullString) (int64, string) {
	if !c.Valid || c.String == "" {
		return 0, ""
	}
	parts := strings.SplitN(c.String, ":", 2)
	if len(parts) != 2 {
		return 0, c.String
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, parts[1]
	}
	return id, parts[1]
}

// handleWorkspaceGet returns the persisted UI state of the current user so the
// frontend can rebuild tabs, the active tab and the layout after a refresh.
func (s *Server) handleWorkspaceGet(w http.ResponseWriter, r *http.Request) {
	state := workspaceStateJSON{
		ActiveTabID: "dashboard",
		Layout:      workspaceLayoutJSON{},
		Tabs:        []workspaceTabJSON{},
	}

	ws, err := s.DB.GetUserWorkspace(r.Context(), db.DefaultUserID)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("Failed to load workspace: %v", err)
		http.Error(w, "Failed to load workspace", http.StatusInternalServerError)
		return
	}
	if err == nil {
		state.ActiveTabID = ws.ActiveTabID
		if ws.LayoutMetadata.Valid {
			_ = json.Unmarshal([]byte(ws.LayoutMetadata.String), &state.Layout)
		}
	}

	tabs, err := s.DB.ListWorkspaceTabs(r.Context(), workspaceUserID())
	if err != nil {
		log.Printf("Failed to load workspace tabs: %v", err)
		http.Error(w, "Failed to load workspace tabs", http.StatusInternalServerError)
		return
	}
	for _, t := range tabs {
		serverID, dbName := decodeConnection(t.ConnectionID)
		state.Tabs = append(state.Tabs, workspaceTabJSON{
			ID:       t.ID,
			Title:    t.Title,
			ServerID: serverID,
			DBName:   dbName,
			Query:    t.QueryText.String,
			TabOrder: int(t.TabOrder),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(state); err != nil {
		log.Printf("Failed to encode workspace: %v", err)
	}
}

// handleWorkspaceSave replaces the persisted UI state of the current user.
func (s *Server) handleWorkspaceSave(w http.ResponseWriter, r *http.Request) {
	var state workspaceStateJSON
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		http.Error(w, "Invalid workspace state", http.StatusBadRequest)
		return
	}

	layoutJSON, err := json.Marshal(state.Layout)
	if err != nil {
		http.Error(w, "Invalid layout state", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	uid := workspaceUserID()

	if err := s.DB.SaveUserWorkspace(ctx, sqlite.SaveUserWorkspaceParams{
		UserID:         db.DefaultUserID,
		ActiveTabID:    state.ActiveTabID,
		LayoutMetadata: sql.NullString{String: string(layoutJSON), Valid: true},
	}); err != nil {
		log.Printf("Failed to save workspace: %v", err)
		http.Error(w, "Failed to save workspace", http.StatusInternalServerError)
		return
	}

	if err := s.DB.ReplaceWorkspaceTabs(ctx, uid); err != nil {
		log.Printf("Failed to replace workspace tabs: %v", err)
		http.Error(w, "Failed to save workspace", http.StatusInternalServerError)
		return
	}

	for _, t := range state.Tabs {
		if err := s.DB.InsertWorkspaceTab(ctx, sqlite.InsertWorkspaceTabParams{
			ID:           t.ID,
			UserID:       uid,
			Title:        t.Title,
			ConnectionID: encodeConnection(t.ServerID, t.DBName),
			QueryText:    sql.NullString{String: t.Query, Valid: t.Query != ""},
			TabOrder:     int64(t.TabOrder),
		}); err != nil {
			log.Printf("Failed to save workspace tab %s: %v", t.ID, err)
			http.Error(w, "Failed to save workspace", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
