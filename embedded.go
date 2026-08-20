// Package embedded exposes repository-root files that the application embeds.
// go:embed can only reference paths inside the source directory, and README.md
// must stay in the repository root to render on GitHub, so this tiny root
// package bridges it to cmd/f4.
package embedded

import _ "embed"

//go:embed README.md
var ReadmeMD string
