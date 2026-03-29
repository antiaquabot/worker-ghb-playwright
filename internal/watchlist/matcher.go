package watchlist

import "github.com/stroi-homes/worker-ghb-playwright/internal/config"

// Matcher matches SSE events against the configured watch list.
type Matcher struct {
	entries []config.WatchEntry
}

func New(entries []config.WatchEntry) *Matcher {
	return &Matcher{entries: entries}
}

// Match returns all watch entries that match the given object external_id.
// "*" in watch_list matches any object.
func (m *Matcher) Match(externalID string) []config.WatchEntry {
	var result []config.WatchEntry
	for _, e := range m.entries {
		if e.ObjectExternalID == "*" || e.ObjectExternalID == externalID {
			result = append(result, e)
		}
	}
	return result
}
