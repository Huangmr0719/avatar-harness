package runtime

import "strings"

const answerMarker = "<speak/>"

// The model explicitly commits to speaking before tools-enabled text is sent to
// TTS. Providers that omit the marker keep the conservative end-of-turn path.
type AnswerGate struct {
	buffer    string
	resolved  bool
	Committed bool
}

func (g *AnswerGate) Feed(text string, final bool) string {
	if g.resolved {
		return text
	}
	g.buffer += text
	trimmed := strings.TrimLeft(g.buffer, " \n\r\t")
	if !final && strings.HasPrefix(answerMarker, trimmed) && len(trimmed) < len(answerMarker) {
		return ""
	}
	g.resolved = true
	output := g.buffer
	if strings.HasPrefix(trimmed, answerMarker) {
		g.Committed = true
		output = strings.TrimPrefix(trimmed, answerMarker)
	}
	g.buffer = ""
	return output
}
