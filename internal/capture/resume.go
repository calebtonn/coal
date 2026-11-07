package capture

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/calebtonn/coal/internal/manifest"
)

// MaxContext caps additionalContext, in characters.
const MaxContext = 4000

type resumeJSON struct {
	SystemMessage      string `json:"systemMessage"`
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// ResumeOutput builds the SessionStart hook output for a matched
// manifest (#3): a systemMessage the user sees before typing, and
// additionalContext the model reads before the first prompt. shortID is
// the id prefix shown in the rename hint; the caller picks one that is
// unique among all manifests, and an empty value means the full id.
func ResumeOutput(m *manifest.Manifest, shortID string) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("resume: no manifest")
	}
	var out resumeJSON
	out.SystemMessage = ResumeLine(m, shortID)
	out.HookSpecificOutput.HookEventName = "SessionStart"
	out.HookSpecificOutput.AdditionalContext = resumeContext(m)
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ResumeLine is the one-line systemMessage banner shown in the terminal
// (F5): "COAL: <summary>. Next: <nextStep>. <n> open questions." The
// next step and the question count are left out when there are none.
func ResumeLine(m *manifest.Manifest, shortID string) string {
	var b strings.Builder
	b.WriteString("COAL: " + sentence(m.Intent.Summary))
	if next := sentence(m.State.NextStep); next != "" {
		b.WriteString(" Next: " + next)
	}
	switch n := len(m.State.OpenQuestions); n {
	case 0:
	case 1:
		b.WriteString(" 1 open question.")
	default:
		fmt.Fprintf(&b, " %d open questions.", n)
	}
	if m.Intent.Source == manifest.SourceModel {
		if shortID == "" {
			shortID = m.ID
		}
		fmt.Fprintf(&b, ` coal rename %s "..." to fix the summary.`, shortID)
	}
	return b.String()
}

// sentence is s on one line, cut to 200 characters, ending in exactly
// one full stop unless it already ends in "?" or "!". Empty stays empty.
func sentence(s string) string {
	s = strings.TrimRight(oneLine(s, 200), ". ")
	if s == "" {
		return ""
	}
	if strings.HasSuffix(s, "?") || strings.HasSuffix(s, "!") {
		return s
	}
	return s + "."
}

func resumeContext(m *manifest.Manifest) string {
	var head strings.Builder
	fmt.Fprintf(&head, "COAL has a record of earlier work in this project (manifest %s).\n", m.ID)
	field := func(label, v string, n int) {
		if v = oneLine(v, n); v != "" {
			fmt.Fprintf(&head, "%s: %s\n", label, v)
		}
	}
	field("Intent", m.Intent.Summary, 300)
	field("Goal", m.Intent.Goal, 300)
	field("Where it stands", m.State.Summary, 400)
	field("Next step", m.State.NextStep, 300)
	if q := m.State.OpenQuestions; len(q) > 0 {
		head.WriteString("Open questions:\n")
		for i, s := range q {
			if i == 5 {
				fmt.Fprintf(&head, "- and %d more\n", len(q)-5)
				break
			}
			fmt.Fprintf(&head, "- %s\n", oneLine(s, 200))
		}
	}

	var tail strings.Builder
	if m.State.NextStep != "" {
		tail.WriteString("Open your first reply by offering, in one sentence, to continue from the next step.\n")
	} else {
		tail.WriteString("Open your first reply by offering, in one sentence, to continue this work.\n")
	}
	tail.WriteString("When you finish a piece of work or the user changes direction, call the coal_update_state MCP tool with the next step, if that tool is available.\n")

	budget := MaxContext - len([]rune(head.String())) - len([]rune(tail.String()))
	var arts strings.Builder
	if len(m.Artifacts) > 0 {
		arts.WriteString("Artifacts:\n")
		for i, a := range m.Artifacts {
			line := "- " + oneLine(a.Title, 100)
			if a.URI != "" {
				line += " (" + oneLine(a.URI, 300) + ")"
			}
			line += "\n"
			more := fmt.Sprintf("- and %d more\n", len(m.Artifacts)-i)
			if len([]rune(arts.String()))+len([]rune(line))+len(more) > budget {
				arts.WriteString(more)
				break
			}
			arts.WriteString(line)
		}
		if len([]rune(arts.String())) > budget {
			arts.Reset()
		}
	}
	return truncate(head.String()+arts.String()+tail.String(), MaxContext)
}

// oneLine collapses whitespace and cuts to n characters on a word boundary.
func oneLine(s string, n int) string {
	return trimWords(strings.Join(strings.Fields(s), " "), n)
}
