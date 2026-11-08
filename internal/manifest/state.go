package manifest

import "slices"

// StateChanged reports whether after differs from before in what a writer
// records on purpose: nextStep, summary or openQuestions. openQuestions
// compares element by element, so nil and [] are the same. lastActivityAt
// and Extra are ignored, so a bumped timestamp alone is no change. The
// daemon's PATCH and the MCP server's coal_update_state both use it to
// decide whether to bump lastActivityAt and stamp the state.
func StateChanged(before, after State) bool {
	return before.NextStep != after.NextStep || before.Summary != after.Summary ||
		!slices.Equal(before.OpenQuestions, after.OpenQuestions)
}
