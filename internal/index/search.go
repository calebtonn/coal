package index

import (
	"strings"
	"unicode"
)

// SearchOpts filters and limits a search.
type SearchOpts struct {
	Status string // empty means any status
	Limit  int    // default DefaultLimit, capped at MaxLimit
}

// Limits for SearchOpts.Limit.
const (
	DefaultLimit = 20
	MaxLimit     = 200
)

// Result is one search hit, in the shape of GET /api/manifests.
type Result struct {
	ID             string   `json:"id"`
	Summary        string   `json:"summary"`
	Source         string   `json:"source"`
	Status         string   `json:"status"`
	LastActivityAt string   `json:"lastActivityAt"`
	UpdatedAt      string   `json:"updatedAt"`
	ArtifactCount  int      `json:"artifactCount"`
	NextStep       string   `json:"nextStep"`
	Snippet        string   `json:"snippet"` // HTML-escaped; <b> is the only markup
	Matched        []string `json:"matched"`
	Rank           float64  `json:"rank"` // bm25, lower is better; 0 when listing
}

// columns are the FTS columns after id, with the names Matched reports.
var columns = []string{"summary", "goal", "tags", "artifacts", "nextStep", "stateSummary"}

// ftsQuery turns user text into a safe FTS5 query: each term reduced to
// letters, digits, '-' and '_', quoted, ANDed, the last one a prefix.
// It returns "" when no usable term remains.
func ftsQuery(q string) string {
	var terms []string
	for _, f := range strings.Fields(q) {
		t := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
				return r
			}
			return -1
		}, f)
		// A term of only '-' and '_' tokenizes to nothing; drop it.
		if strings.IndexFunc(t, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			terms = append(terms, `"`+t+`"`)
		}
	}
	if len(terms) == 0 {
		return ""
	}
	terms[len(terms)-1] += "*"
	return strings.Join(terms, " AND ")
}

// Search lists manifests matching q, best first. An empty q lists all
// manifests, newest activity first.
func (ix *Index) Search(q string, opts SearchOpts) ([]Result, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	results := []Result{}

	if strings.TrimSpace(q) == "" {
		rows, err := ix.db.Query(`SELECT id, summary, source, status, last_activity_at, updated_at, artifact_count, next_step
			FROM docs WHERE (?1 = '' OR status = ?1)
			ORDER BY last_activity_at DESC, id DESC LIMIT ?2`, opts.Status, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			r := Result{Matched: []string{}}
			if err := rows.Scan(&r.ID, &r.Summary, &r.Source, &r.Status, &r.LastActivityAt, &r.UpdatedAt, &r.ArtifactCount, &r.NextStep); err != nil {
				return nil, err
			}
			results = append(results, r)
		}
		return results, rows.Err()
	}

	match := ftsQuery(q)
	if match == "" {
		return results, nil
	}
	// The inner query ranks and limits; snippet and highlight run only on
	// the rows that survive. bm25 weights cover every column in order, so
	// the unindexed id column gets 0.
	rows, err := ix.db.Query(`
		WITH hits AS (
			SELECT fts.rowid AS rid, bm25(fts, 0.0, 10.0, 4.0, 3.0, 2.0, 2.0, 1.0) AS rank
			FROM fts JOIN docs ON docs.rowid = fts.rowid
			WHERE fts MATCH ?1 AND (?2 = '' OR docs.status = ?2)
			ORDER BY rank, docs.source = 'user' DESC, docs.last_activity_at DESC, docs.id DESC
			LIMIT ?3)
		SELECT d.id, d.summary, d.source, d.status, d.last_activity_at, d.updated_at, d.artifact_count, d.next_step,
			hits.rank, snippet(fts, -1, ?4, ?5, '…', 12),
			highlight(fts, 1, ?4, ?5), highlight(fts, 2, ?4, ?5), highlight(fts, 3, ?4, ?5),
			highlight(fts, 4, ?4, ?5), highlight(fts, 5, ?4, ?5), highlight(fts, 6, ?4, ?5)
		FROM hits JOIN fts ON fts.rowid = hits.rid JOIN docs d ON d.rowid = hits.rid
		WHERE fts MATCH ?1
		ORDER BY hits.rank, d.source = 'user' DESC, d.last_activity_at DESC, d.id DESC`,
		match, opts.Status, limit, markOpen, markClose)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r := Result{Matched: []string{}}
		hl := make([]string, len(columns))
		dest := []any{&r.ID, &r.Summary, &r.Source, &r.Status, &r.LastActivityAt, &r.UpdatedAt, &r.ArtifactCount, &r.NextStep, &r.Rank, &r.Snippet}
		for i := range hl {
			dest = append(dest, &hl[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		r.Snippet = safeSnippet(r.Snippet)
		for i, h := range hl {
			if strings.Contains(h, markOpen) {
				r.Matched = append(r.Matched, columns[i])
			}
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
