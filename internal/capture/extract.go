package capture

import (
	"regexp"
	"strings"
)

// MaxStateSummary is the longest state summary taken from a message.
const MaxStateSummary = 300

var (
	leadMarkers = regexp.MustCompile(`^(?:[-*+]\s+|\d+[.)]\s+)?(?:\*\*|__)?`)
	nextLabel   = regexp.MustCompile(`(?i)^next(?:\s+steps?)?\s*(?:\*\*|__)?\s*:\s*(?:\*\*|__)?\s*`)
)

// ExtractState is the sessionEnd fallback (#2) for when the model did not
// record state itself. summary is the first paragraph of the last
// assistant message, cut to 300 characters on a word boundary. nextStep
// comes only from the last line that starts with "Next step", "Next
// steps" or "Next:", optionally after a bullet or bold marker; when the
// label ends the line, the first item on the following line is used.
func ExtractState(lastAssistantMessage string) (summary, nextStep string) {
	msg := strings.ReplaceAll(lastAssistantMessage, "\r\n", "\n")
	for _, para := range strings.Split(msg, "\n\n") {
		if p := strings.Join(strings.Fields(para), " "); p != "" {
			summary = trimWords(p, MaxStateSummary)
			break
		}
	}
	lines := strings.Split(msg, "\n")
	for i, l := range lines {
		t := leadMarkers.ReplaceAllString(strings.TrimSpace(l), "")
		loc := nextLabel.FindStringIndex(t)
		if loc == nil {
			continue
		}
		step := t[loc[1]:]
		if strings.TrimSpace(step) == "" {
			for _, n := range lines[i+1:] {
				if n = strings.TrimSpace(n); n != "" {
					step = leadMarkers.ReplaceAllString(n, "")
					break
				}
			}
		}
		if step = cleanStep(step); step != "" {
			nextStep = step
		}
	}
	return summary, nextStep
}

func cleanStep(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSuffix(strings.TrimSuffix(s, "**"), "__")
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	return trimWords(s, MaxStateSummary)
}
