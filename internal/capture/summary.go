package capture

import (
	"regexp"
	"strings"
	"unicode"
)

// MaxSummary is the longest intent summary taken from a prompt.
const MaxSummary = 200

var pastedBlock = regexp.MustCompile(`(?s)<pasted_content[^>]*>.*?</pasted_content[^>]*>`)

// stopPhrases are whole prompts that say "carry on" and nothing else.
var stopPhrases = map[string]bool{}

// fillerWords make up prompts like "yes please go ahead and continue".
var fillerWords = map[string]bool{}

func init() {
	for _, p := range []string{
		"continue", "yes", "go on", "ok", "okay", "next", "proceed", "keep going",
		"do it", "go ahead", "carry on", "sure", "yep", "yeah", "y", "go",
		"please continue", "continue please", "sounds good", "looks good", "lgtm",
		"thanks", "thank you", "resume", "start", "do that", "yes please",
	} {
		stopPhrases[p] = true
	}
	for _, w := range strings.Fields(`continue yes yep yeah ok okay go on ahead next
		proceed keep going do it that please sure carry and then now thanks thank you
		lgtm sounds good looks great fine alright right resume start just`) {
		fillerWords[w] = true
	}
}

// PickSummary applies the summary filter (#11) to a user prompt. It
// rejects slash commands, prompts under four words, stop-list replies
// and text that is mostly code or a stack trace. On accept it returns
// the first plain line, whitespace collapsed, cut to 200 characters on a
// word boundary.
func PickSummary(prompt string) (string, bool) {
	text := strings.TrimSpace(pastedBlock.ReplaceAllString(prompt, ""))
	if text == "" || strings.HasPrefix(text, "/") {
		return "", false
	}
	if isStopPhrase(text) {
		return "", false
	}
	var plain []string
	code, lines := 0, 0
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines++
		if looksLikeCode(l) {
			code++
			continue
		}
		plain = append(plain, strings.Join(strings.Fields(l), " "))
	}
	if lines == 0 || code*10 > lines*4 {
		return "", false
	}
	if len(strings.Fields(strings.Join(plain, " "))) < 4 {
		return "", false
	}
	pick := strings.Join(plain, " ")
	for _, l := range plain {
		if len(strings.Fields(l)) >= 4 {
			pick = l
			break
		}
	}
	return trimWords(pick, MaxSummary), true
}

// isStopPhrase reports whether text, ignoring case and punctuation, is
// on the stop list or made only of filler words.
func isStopPhrase(text string) bool {
	norm := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, text)
	words := strings.Fields(norm)
	if stopPhrases[strings.Join(words, " ")] {
		return true
	}
	for _, w := range words {
		if !fillerWords[w] {
			return false
		}
	}
	return true
}

var (
	stackFrame = regexp.MustCompile(`^at \S+.*[(:]`)
	errorHead  = regexp.MustCompile(`^[A-Za-z_.]*(Error|Exception|Panic)\b.*:`)
)

// looksLikeCode reports whether one line reads as code, a log line or a
// stack frame rather than prose.
func looksLikeCode(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	first := []rune(t)[0]
	indented := len(t) < len(line) && unicode.IsSpace([]rune(line)[0])
	if indented && !unicode.IsLetter(first) && !unicode.IsDigit(first) && !strings.ContainsRune("-*+>•\"'", first) {
		return true
	}
	if stackFrame.MatchString(t) || errorHead.MatchString(t) ||
		strings.Contains(t, "Traceback") || strings.HasPrefix(t, `File "`) ||
		strings.HasPrefix(t, "goroutine ") || strings.HasPrefix(t, "```") {
		return true
	}
	last := t[len(t)-1]
	if last == '{' || last == '}' || last == ';' || first == '}' {
		return true
	}
	sym := 0
	for _, r := range t {
		if strings.ContainsRune("{}[]();=<>$#|&", r) {
			sym++
		}
	}
	return sym >= 3 && sym*5 > len([]rune(t))
}
