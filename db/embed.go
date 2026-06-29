// Package migrations embeds the SQL migration files so the binary is fully
// self-contained — one file to deploy, no loose .sql to ship alongside it.
package migrations

import "embed"

//go:embed migrations/*.sql
var FS embed.FS
