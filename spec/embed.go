// Package spec embeds the Intent Manifest JSON Schema so Go code can use it.
package spec

import _ "embed"

// IntentManifestSchema is the JSON Schema 2020-12 document for Intent Manifest v0.1.
//
//go:embed intent-manifest.schema.json
var IntentManifestSchema []byte
