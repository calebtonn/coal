package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/metrics"
	"github.com/calebtonn/coal/internal/store"
)

func init() { register(purgeCmd) }

const purgedReason = "purged"

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// purgeRef is one artifact that points at the purged body.
type purgeRef struct {
	id, summary, title string
	versions           []int // 1-based version numbers with the hash
	latest             bool  // the artifact's current body is the hash
}

// purgeRefs lists every artifact and version in ms that references hash.
func purgeRefs(ms []*manifest.Manifest, hash string) []purgeRef {
	var refs []purgeRef
	for _, m := range ms {
		for _, art := range m.Artifacts {
			r := purgeRef{id: m.ID, summary: m.Intent.Summary, title: art.Title, latest: art.Hash == hash}
			for i, v := range art.Versions {
				if v.Hash == hash {
					r.versions = append(r.versions, i+1)
				}
			}
			if r.latest || len(r.versions) > 0 {
				refs = append(refs, r)
			}
		}
	}
	return refs
}

// markPurged sets skipped on every version with hash, and on the artifact
// when its current body is hash. It reports whether anything changed.
func markPurged(m *manifest.Manifest, hash string) bool {
	changed := false
	for i := range m.Artifacts {
		art := &m.Artifacts[i]
		for j := range art.Versions {
			if v := &art.Versions[j]; v.Hash == hash && v.Skipped != purgedReason {
				v.Skipped, changed = purgedReason, true
			}
		}
		if art.Hash == hash && art.Skipped != purgedReason {
			art.Skipped, changed = purgedReason, true
		}
	}
	return changed
}

func purgeCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "purge <hash>",
		Short: "Delete an artifact body that holds a secret (the one exception to never-delete)",
		Long: "Remove artifacts/<hex> and any open copy of it, and mark every artifact version that\n" +
			"used it as skipped: \"purged\". Manifests are kept. Asks for \"yes\" unless --yes is given.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hexHash := strings.ToLower(strings.TrimPrefix(args[0], "sha256:"))
			if !hex64.MatchString(hexHash) {
				return fmt.Errorf("%q is not a full sha256 artifact hash (64 hex characters, optionally prefixed sha256:)", args[0])
			}
			hash := "sha256:" + hexHash
			a, _, err := openApp("")
			if err != nil {
				return err
			}
			defer a.Close()
			ms, _ := a.Store.List()
			refs := purgeRefs(ms, hash)
			body := a.Store.ArtifactPath(hash)
			copies, _ := filepath.Glob(filepath.Join(a.Store.Dir(), "tmp", hexHash[:8]+"-*"))
			st, statErr := os.Stat(body)
			if statErr != nil && len(refs) == 0 && len(copies) == 0 {
				return fmt.Errorf("nothing to purge: no body and no manifest references %s", hash)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "This permanently deletes the body of %s.\n", hash)
			if statErr == nil {
				fmt.Fprintf(out, "  body: %s (%d bytes)\n", body, st.Size())
			} else {
				fmt.Fprintf(out, "  body: %s (already gone)\n", body)
			}
			for _, c := range copies {
				fmt.Fprintf(out, "  open copy: %s\n", c)
			}
			for _, r := range refs {
				where := fmt.Sprintf("versions %v", r.versions)
				if r.latest {
					where += ", current body"
				}
				fmt.Fprintf(out, "  manifest %s %q, artifact %q: %s\n", r.id, truncate(oneLine(r.summary), 40), r.title, where)
			}
			fmt.Fprintf(out, "Each of those versions will be marked skipped: %q. No manifest is deleted.\n", purgedReason)
			if !yes {
				fmt.Fprint(out, "Type yes to continue: ")
				line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if strings.TrimSpace(line) != "yes" {
					return errors.New("not purged")
				}
			}

			// Mark first, then delete: if a step fails, running purge again
			// finishes the job, and no manifest points at a body silently gone.
			marked := map[string]bool{}
			for _, r := range refs {
				if marked[r.id] {
					continue
				}
				_, err := a.Update(r.id, 0, func(m *manifest.Manifest) error {
					if !markPurged(m, hash) {
						return store.ErrNoChange
					}
					return nil
				}, store.Op{Tool: "coal-cli", Action: "purge"})
				if err != nil {
					return fmt.Errorf("marking %s: %w; nothing was deleted", r.id, err)
				}
				marked[r.id] = true
			}
			for _, p := range append(copies, body) {
				if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			}
			a.Store.Log("purge %s", hash)
			metrics.Record(a.Store.Dir(), metrics.Purge, map[string]any{"manifests": len(marked)})
			fmt.Fprintf(out, "purged %s from %d manifests\n", hash, len(marked))
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return cmd
}
