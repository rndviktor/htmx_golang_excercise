-- Minimal stubs for PostgreSQL system catalog tables referenced by queries.sql.
-- These tables exist in every PostgreSQL instance; the stubs let sqlc parse the
-- queries and generate correct Go return types.

CREATE TABLE pg_database (
    oid SERIAL PRIMARY KEY,
    datname TEXT NOT NULL,
    datallowconn BOOLEAN NOT NULL,
    datistemplate BOOLEAN NOT NULL
);

CREATE TABLE pg_roles (
    rolname TEXT NOT NULL
);

CREATE TABLE pg_tablespace (
    spcname TEXT NOT NULL
);

CREATE TABLE pg_cast (
    castsource INTEGER NOT NULL,
    casttarget INTEGER NOT NULL
);

CREATE TABLE pg_namespace (
    oid SERIAL PRIMARY KEY,
    nspname TEXT NOT NULL
);

CREATE TABLE pg_event_trigger (
    evtname TEXT NOT NULL
);

CREATE TABLE pg_extension (
    extname TEXT NOT NULL
);

CREATE TABLE pg_foreign_data_wrapper (
    fdwname TEXT NOT NULL
);

CREATE TABLE pg_language (
    lanname TEXT NOT NULL,
    lanispl BOOLEAN NOT NULL
);

CREATE TABLE pg_publication (
    pubname TEXT NOT NULL
);

CREATE TABLE pg_subscription (
    subname TEXT NOT NULL,
    subdbid INTEGER NOT NULL
);

CREATE TABLE pg_tables (
    tablename TEXT NOT NULL,
    schemaname TEXT NOT NULL,
    tableowner TEXT NOT NULL,
    tablespace TEXT
);

CREATE TABLE pg_views (
    viewname TEXT NOT NULL,
    schemaname TEXT NOT NULL
);

CREATE TABLE pg_matviews (
    matviewname TEXT NOT NULL,
    schemaname TEXT NOT NULL
);

CREATE TABLE pg_sequences (
    sequencename TEXT NOT NULL,
    schemaname TEXT NOT NULL
);

CREATE TABLE pg_proc (
    oid SERIAL PRIMARY KEY,
    proname TEXT NOT NULL,
    pronamespace INTEGER NOT NULL,
    prokind CHAR NOT NULL
);

CREATE TABLE pg_type (
    typname TEXT NOT NULL,
    typtype CHAR NOT NULL,
    typnamespace INTEGER NOT NULL
);

CREATE TABLE pg_class (
    oid SERIAL PRIMARY KEY,
    relname TEXT NOT NULL,
    relnamespace INTEGER NOT NULL
);

CREATE TABLE pg_constraint (
    conname TEXT NOT NULL,
    contype CHAR NOT NULL,
    conrelid INTEGER NOT NULL,
    conkey INTEGER[]
);

CREATE TABLE pg_attribute (
    attrelid INTEGER NOT NULL,
    attname TEXT NOT NULL,
    attnum INTEGER NOT NULL,
    atttypid INTEGER NOT NULL,
    attcollation INTEGER NOT NULL
);

CREATE TABLE pg_collation (
    oid INTEGER NOT NULL,
    collname TEXT NOT NULL
);

CREATE TABLE pg_trigger (
    tgname TEXT NOT NULL,
    tgrelid INTEGER NOT NULL,
    tgisinternal BOOLEAN NOT NULL
);

CREATE TABLE pg_indexes (
    indexname TEXT NOT NULL,
    schemaname TEXT NOT NULL,
    tablename TEXT NOT NULL
);

CREATE TABLE pg_policies (
    policyname TEXT NOT NULL,
    schemaname TEXT NOT NULL,
    tablename TEXT NOT NULL
);

CREATE TABLE pg_rules (
    rulename TEXT NOT NULL,
    schemaname TEXT NOT NULL,
    tablename TEXT NOT NULL
);


