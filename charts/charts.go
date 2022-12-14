package charts

import "embed"

var (
	// Go Embed happens to ignore files starting with _, e.g. `_helpers.tpl`.
	// Using the `all` modifier for the embed directive fixes this.

	//go:embed all:seed
	Seed embed.FS
)
