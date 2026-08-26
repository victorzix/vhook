// Package migrations embeds the SQL files so the api applies them from its own
// binary at boot. It is a package only because go:embed cannot reach above the
// directory of the file that declares it.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
