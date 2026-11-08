package main

import (
	"fmt"
	"strings"

	"github.com/calebtonn/coal/internal/app"
)

// resolve turns an id or unique id prefix into an id. When a prefix
// matches several manifests, the error lists them so the user can pick.
func resolve(a *app.App, arg string) (string, error) {
	id, err := a.Resolve(arg)
	if err == nil {
		return id, nil
	}
	prefix := strings.ToLower(arg)
	var b strings.Builder
	n := 0
	if all, ierr := a.Index.IDs(); ierr == nil && len(prefix) >= 8 {
		for _, c := range all {
			if !strings.HasPrefix(c, prefix) {
				continue
			}
			n++
			summary := ""
			if m, gerr := a.Get(c); gerr == nil {
				summary = truncate(oneLine(m.Intent.Summary), 60)
			}
			fmt.Fprintf(&b, "\n  %s  %s", c, summary)
		}
	}
	if n < 2 {
		return "", err
	}
	return "", fmt.Errorf("id prefix %s matches %d manifests; give more of the id:%s", prefix, n, b.String())
}
