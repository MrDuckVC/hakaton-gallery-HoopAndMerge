// Package migrations embeds the immutable SQL migration set shipped with the binary.
package migrations

import "embed"

//go:embed *.sql
//nolint:gochecknoglobals // go:embed requires a package-level variable.
var files embed.FS

// Files returns the embedded append-only migration sources.
func Files() embed.FS { return files }
