package config

import _ "embed"

// fallbackConfigs is embedded at build time as a last-resort fallback
// when network and cache are both unavailable (first run under blockade).
//
// Update with: go generate ./internal/config/...
// Or manually: ./scripts/update-fallback.sh
//
//go:generate go run ../../scripts/gen_fallback.go
//go:embed fallback.txt
var fallbackConfigs string
