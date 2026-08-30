package sqlc

import _ "embed"

// Schema is the SQLite DDL applied at startup. It is embedded from the same
// schema.sql that sqlc uses for code generation.
//
//go:embed schema.sql
var Schema string