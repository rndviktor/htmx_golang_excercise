-- Server-level queries (run against maintenance DB)

-- name: ListDatabases :many
SELECT datname FROM pg_database
WHERE datallowconn AND NOT datistemplate
ORDER BY datname;

-- name: ListRoles :many
SELECT rolname FROM pg_roles
ORDER BY rolname;

-- name: ListTablespaces :many
SELECT spcname FROM pg_tablespace
ORDER BY spcname;

-- Database-level category queries (run against a specific database)

-- name: ListCasts :many
SELECT '(' || castsource::regtype || ' AS ' || casttarget::regtype || ')' FROM pg_cast ORDER BY 1;

-- name: ListCatalogs :many
SELECT nspname FROM pg_namespace
WHERE nspname = 'information_schema' OR nspname LIKE 'pg\_%'
ORDER BY 1;

-- name: ListEventTriggers :many
SELECT evtname FROM pg_event_trigger ORDER BY 1;

-- name: ListExtensions :many
SELECT extname FROM pg_extension ORDER BY 1;

-- name: ListForeignDataWrappers :many
SELECT fdwname FROM pg_foreign_data_wrapper ORDER BY 1;

-- name: ListLanguages :many
SELECT lanname FROM pg_language WHERE lanispl ORDER BY 1;

-- name: ListPublications :many
SELECT pubname FROM pg_publication ORDER BY 1;

-- name: ListSchemas :many
SELECT nspname FROM pg_namespace
WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema'
ORDER BY 1;

-- name: ListSubscriptions :many
SELECT subname FROM pg_subscription
WHERE subdbid = (SELECT oid FROM pg_database WHERE datname = current_database())
ORDER BY 1;

-- Schema-level category queries ($1 = schema name)

-- name: ListTables :many
SELECT tablename FROM pg_tables WHERE schemaname = $1 ORDER BY 1;

-- name: ListViews :many
SELECT viewname FROM pg_views WHERE schemaname = $1 ORDER BY 1;

-- name: ListMaterializedViews :many
SELECT matviewname FROM pg_matviews WHERE schemaname = $1 ORDER BY 1;

-- name: ListSequences :many
SELECT sequencename FROM pg_sequences WHERE schemaname = $1 ORDER BY 1;

-- name: ListFunctions :many
SELECT p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')'
FROM pg_proc p
JOIN pg_namespace n ON p.pronamespace = n.oid
WHERE n.nspname = $1 AND p.prokind = 'f'
ORDER BY 1;

-- name: ListProcedures :many
SELECT p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')'
FROM pg_proc p
JOIN pg_namespace n ON p.pronamespace = n.oid
WHERE n.nspname = $1 AND p.prokind = 'p'
ORDER BY 1;

-- name: ListTypes :many
SELECT t.typname FROM pg_type t
JOIN pg_namespace n ON t.typnamespace = n.oid
WHERE n.nspname = $1 AND t.typtype IN ('c', 'e', 'r')
ORDER BY 1;

-- name: ListDomains :many
SELECT t.typname FROM pg_type t
JOIN pg_namespace n ON t.typnamespace = n.oid
WHERE n.nspname = $1 AND t.typtype = 'd'
ORDER BY 1;

-- Table-level category queries ($1 = schema name, $2 = table name)

-- name: GetTableColumns :many
SELECT column_name FROM information_schema.columns
WHERE table_schema = $1 AND table_name = $2
ORDER BY ordinal_position;

-- name: ListTableColumns :many
SELECT column_name || ' ' || data_type FROM information_schema.columns
WHERE table_schema = $1 AND table_name = $2
ORDER BY ordinal_position;

-- name: ListConstraints :many
SELECT c.conname || ' (' || CASE c.contype
    WHEN 'p' THEN 'primary key'
    WHEN 'f' THEN 'foreign key'
    WHEN 'u' THEN 'unique'
    WHEN 'c' THEN 'check'
    ELSE c.contype::text
END || ')'
FROM pg_constraint c
JOIN pg_class t ON c.conrelid = t.oid
JOIN pg_namespace n ON t.relnamespace = n.oid
WHERE n.nspname = $1 AND t.relname = $2
ORDER BY 1;

-- name: ListTableIndexes :many
SELECT indexname FROM pg_indexes
WHERE schemaname = $1 AND tablename = $2
ORDER BY 1;

-- name: ListPolicies :many
SELECT policyname FROM pg_policies
WHERE schemaname = $1 AND tablename = $2
ORDER BY 1;

-- name: ListTableRules :many
SELECT rulename FROM pg_rules
WHERE schemaname = $1 AND tablename = $2
ORDER BY 1;

-- name: ListTableTriggers :many
SELECT tg.tgname FROM pg_trigger tg
JOIN pg_class t ON tg.tgrelid = t.oid
JOIN pg_namespace n ON t.relnamespace = n.oid
WHERE n.nspname = $1 AND t.relname = $2 AND NOT tg.tgisinternal
ORDER BY 1;
