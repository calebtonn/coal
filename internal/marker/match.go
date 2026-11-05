package marker

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/calebtonn/coal/internal/manifest"
)

// Match picks the manifest a session in cwd belongs to, from the active
// and paused candidates (ADR-013, #4). In order: the manifest named by
// the nearest marker that names a candidate; a path context the resolved cwd is at or under,
// longest key first; a repo context equal to cwd's origin remote. A
// path key at or above the home directory never matches, so a session
// in ~ or / attaches to nothing by path. Ties go to the most recently
// active manifest. It returns nil when nothing matches.
func Match(cwd string, candidates []*manifest.Manifest) *manifest.Manifest {
	var live []*manifest.Manifest
	for _, m := range candidates {
		if m != nil && (m.Status == manifest.StatusActive || m.Status == manifest.StatusPaused) {
			live = append(live, m)
		}
	}
	if len(live) == 0 {
		return nil
	}
	var marked *manifest.Manifest
	walk(cwd, func(id, _ string) bool {
		for _, m := range live {
			if m.ID == id {
				marked = m
				return false
			}
		}
		return true
	})
	if marked != nil {
		return marked
	}

	dir := resolve(cwd)
	home := ""
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		home = resolve(h)
	}
	var best *manifest.Manifest
	bestLen := 0
	for _, m := range live {
		for _, c := range m.Contexts {
			if c.Kind != KindPath || !filepath.IsAbs(c.Key) {
				continue
			}
			key := filepath.Clean(c.Key)
			if key == "/" || (home != "" && under(home, key)) {
				continue
			}
			if under(dir, key) && (len(key) > bestLen || len(key) == bestLen && newer(m, best)) {
				best, bestLen = m, len(key)
			}
		}
	}
	if best != nil {
		return best
	}

	remote := OriginRemote(ProjectRoot(cwd))
	if remote == "" {
		return nil
	}
	for _, m := range live {
		for _, c := range m.Contexts {
			if c.Kind == KindRepo && NormalizeRemote(c.Key) == remote && newer(m, best) {
				best = m
			}
		}
	}
	return best
}

// under reports whether p equals dir or sits below it.
func under(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+"/")
}

// newer reports whether a was active more recently than b.
func newer(a, b *manifest.Manifest) bool {
	return b == nil || a.State.LastActivityAt > b.State.LastActivityAt
}
