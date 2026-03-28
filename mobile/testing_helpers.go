package mobile

import "github.com/mewmewmemw/autovpn/internal/config"

// configSourcesSnapshot returns a copy of current ConfigSources.
func configSourcesSnapshot() []string {
	cp := make([]string, len(config.ConfigSources))
	copy(cp, config.ConfigSources)
	return cp
}

// setConfigSources overrides ConfigSources for testing.
func setConfigSources(urls []string) {
	config.ConfigSources = urls
}
