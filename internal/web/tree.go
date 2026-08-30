package web

import (
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"htmx-golang-excercise/internal/db"
	sqlite "htmx-golang-excercise/internal/sqlc/sqlite/db"
)

// treeNode is one row of the object explorer tree. Nodes with a URL are
// lazy-loaded expanders whose children are fetched from that endpoint;
// nodes without a URL render as plain leaves.
type treeNode struct {
	ID    string // DOM id of the children container (expanders only)
	Icon  string
	Label string
	Badge string // optional counter shown next to the label
	Sub   string // optional secondary line under the label
	URL   string // htmx GET url for lazy children; empty means leaf node
	Menu  string // context menu kind for right click (e.g. "table"); empty = none
}

// expander builds a lazy-loadable tree node whose children are fetched from
// url into the container named id.
func expander(id, icon, label, url string) treeNode {
	return treeNode{ID: id, Icon: icon, Label: label, URL: url}
}

// leaves converts plain item names into non-expandable tree nodes.
func leaves(icon string, names []string) []treeNode {
	nodes := make([]treeNode, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, treeNode{Icon: icon, Label: name})
	}
	return nodes
}

// categoryFolders builds the expander nodes for the category folders under a
// parent node. prefix is the DOM id prefix and baseURL the route prefix of
// the parent, e.g. "database-1-postgres" and "/api/servers/1/databases/postgres".
func categoryFolders(prefix, baseURL string, cats []category) []treeNode {
	nodes := make([]treeNode, 0, len(cats))
	for _, c := range cats {
		nodes = append(nodes, expander(
			prefix+"-"+c.Slug,
			c.Icon,
			c.Label,
			baseURL+"/"+c.Slug,
		))
	}
	return nodes
}

// renderTree responds with the single tree fragment shared by all tree endpoints.
func renderTree(w http.ResponseWriter, nodes []treeNode, emptyMessage string) {
	RenderPartial(w, "tree_nodes.html", map[string]any{
		"Nodes":        nodes,
		"EmptyMessage": emptyMessage,
	})
}

// listServers fetches the registered servers of the default user group.
// On failure it writes the error response itself and returns ok=false.
func (s *Server) listServers(w http.ResponseWriter, r *http.Request) ([]sqlite.ListServersByGroupRow, bool) {
	servers, err := s.DB.ListServersByGroup(r.Context(), db.DefaultUserID)
	if err != nil {
		log.Printf("Failed to list servers: %v", err)
		http.Error(w, "Failed to load servers", http.StatusInternalServerError)
		return nil, false
	}
	return servers, true
}

// queryNames runs query against pool and collects the first column of every
// row as strings. On failure it writes the error response itself and returns
// ok=false.
func (s *Server) queryNames(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool, what, query string, args ...any) ([]string, bool) {
	rows, err := pool.Query(r.Context(), query, args...)
	if err != nil {
		log.Printf("Failed to query %s: %v", what, err)
		http.Error(w, "Failed to load "+what, http.StatusInternalServerError)
		return nil, false
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			log.Printf("Failed to scan %s: %v", what, err)
			http.Error(w, "Failed to load "+what, http.StatusInternalServerError)
			return nil, false
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Failed to iterate %s: %v", what, err)
		http.Error(w, "Failed to load "+what, http.StatusInternalServerError)
		return nil, false
	}

	return names, true
}

// category describes one expandable folder in the tree together with the
// live SQL used to list its contents. Schema-scoped queries take the schema
// name as $1; the rest run without arguments.
type category struct {
	Slug     string
	Label    string
	Query    string
	Icon     string
	Empty    string
	InSchema bool
}

var dbCategories = []category{
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

var schemaCategories = []category{
	{Slug: "tables", Label: "Tables", Icon: "📋", Empty: "No tables found.", InSchema: true,
		Query: `SELECT tablename FROM pg_tables WHERE schemaname = $1 ORDER BY 1`},
	{Slug: "views", Label: "Views", Icon: "🔭", Empty: "No views found.", InSchema: true,
		Query: `SELECT viewname FROM pg_views WHERE schemaname = $1 ORDER BY 1`},
	{Slug: "materialized-views", Label: "Materialized Views", Icon: "🧊", Empty: "No materialized views found.", InSchema: true,
		Query: `SELECT matviewname FROM pg_matviews WHERE schemaname = $1 ORDER BY 1`},
	{Slug: "sequences", Label: "Sequences", Icon: "🔢", Empty: "No sequences found.", InSchema: true,
		Query: `SELECT sequencename FROM pg_sequences WHERE schemaname = $1 ORDER BY 1`},
	{Slug: "functions", Label: "Functions", Icon: "⚙️", Empty: "No functions found.", InSchema: true,
		Query: `SELECT p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')' ` +
			`FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid ` +
			`WHERE n.nspname = $1 AND p.prokind = 'f' ORDER BY 1`},
	{Slug: "procedures", Label: "Procedures", Icon: "🛠️", Empty: "No procedures found.", InSchema: true,
		Query: `SELECT p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')' ` +
			`FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid ` +
			`WHERE n.nspname = $1 AND p.prokind = 'p' ORDER BY 1`},
	{Slug: "types", Label: "Types", Icon: "🏷️", Empty: "No types found.", InSchema: true,
		Query: `SELECT t.typname FROM pg_type t JOIN pg_namespace n ON t.typnamespace = n.oid ` +
			`WHERE n.nspname = $1 AND t.typtype IN ('c', 'e', 'r') ORDER BY 1`},
	{Slug: "domains", Label: "Domains", Icon: "📐", Empty: "No domains found.", InSchema: true,
		Query: `SELECT t.typname FROM pg_type t JOIN pg_namespace n ON t.typnamespace = n.oid ` +
			`WHERE n.nspname = $1 AND t.typtype = 'd' ORDER BY 1`},
}

// tableCategories lists the object folders shown when a table node is
// expanded. Every Query takes the schema name as $1 and the table name as $2.
var tableCategories = []category{
	{Slug: "columns", Label: "Columns", Icon: "📊", Empty: "No columns found.",
		Query: `SELECT column_name || ' ' || data_type FROM information_schema.columns ` +
			`WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position`},
	{Slug: "constraints", Label: "Constraints", Icon: "🔒", Empty: "No constraints found.",
		Query: `SELECT c.conname || ' (' || CASE c.contype WHEN 'p' THEN 'primary key' WHEN 'f' THEN 'foreign key' ` +
			`WHEN 'u' THEN 'unique' WHEN 'c' THEN 'check' ELSE c.contype::text END || ')' ` +
			`FROM pg_constraint c JOIN pg_class t ON c.conrelid = t.oid JOIN pg_namespace n ON t.relnamespace = n.oid ` +
			`WHERE n.nspname = $1 AND t.relname = $2 ORDER BY 1`},
	{Slug: "indexes", Label: "Indexes", Icon: "📇", Empty: "No indexes found.",
		Query: `SELECT indexname FROM pg_indexes WHERE schemaname = $1 AND tablename = $2 ORDER BY 1`},
	{Slug: "rls-policies", Label: "RLS Policies", Icon: "🛡️", Empty: "No RLS policies found.",
		Query: `SELECT policyname FROM pg_policies WHERE schemaname = $1 AND tablename = $2 ORDER BY 1`},
	{Slug: "rules", Label: "Rules", Icon: "📜", Empty: "No rules found.",
		Query: `SELECT rulename FROM pg_rules WHERE schemaname = $1 AND tablename = $2 ORDER BY 1`},
	{Slug: "triggers", Label: "Triggers", Icon: "💥", Empty: "No triggers found.",
		Query: `SELECT tg.tgname FROM pg_trigger tg JOIN pg_class t ON tg.tgrelid = t.oid ` +
			`JOIN pg_namespace n ON t.relnamespace = n.oid ` +
			`WHERE n.nspname = $1 AND t.relname = $2 AND NOT tg.tgisinternal ORDER BY 1`},
}

func findCategory(cats []category, slug string) *category {
	for i := range cats {
		if cats[i].Slug == slug {
			return &cats[i]
		}
	}
	return nil
}
