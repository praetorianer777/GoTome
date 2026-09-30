// Package migrations embeds the SQL migration files, so the binary carries its
// own schema and the image needs no bind mount to apply it.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
