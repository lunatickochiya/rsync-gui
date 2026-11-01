// Package presets contains the built-in rsync-web presets. The JSON files use the
// same format as rsync-gui, so presets and settings files are interchangeable.
package presets

import "embed"

// FS holds the built-in preset files.
//
//go:embed *.json
var FS embed.FS
