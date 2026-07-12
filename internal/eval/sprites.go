package eval

import (
	"embed"
	"encoding/base64"
	"fmt"
)

// The report is a single self-contained HTML file — it must fetch nothing to
// render. Pokémon sprites are therefore vendored here (the PokeAPI front
// sprites, one <national-dex>.png each) and base64-inlined as data: URIs at
// render time, rather than linked from github like the live web app does.
//
//go:embed sprites/*.png
var spriteFS embed.FS

// spriteDataURI returns a base64 "data:" URI for a national-dex number's
// vendored sprite, or "" when that sprite is not embedded (the caller then
// falls back to the type-coloured monogram medallion). Because the bytes are
// inlined, the report needs no network access at build or view time.
func spriteDataURI(dexNo int) string {
	b, err := spriteFS.ReadFile(fmt.Sprintf("sprites/%d.png", dexNo))
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
}
