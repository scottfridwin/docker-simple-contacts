// Package migrations embeds the SQL migration files so they can be applied at
// startup. The same files are used by the golang-migrate CLI in development.
package migrations

import "embed"

// FS holds the embedded migration files.
//
//go:embed *.sql
var FS embed.FS
