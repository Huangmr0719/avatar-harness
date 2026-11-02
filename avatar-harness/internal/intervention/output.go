package intervention

import (
	"regexp"
	"strings"
	"unicode"
)

// ResponseConstraints handles only explicit, current-turn surface constraints.
// It is deliberately not a semantic intent or risk classifier. Quoted and
// reported requests are left to the reviewer rather than executed as commands.
type ResponseConstraints struct {
	Sentences     int  `json:"sentences,omitempty"`
	NoQuestions   bool `json:"no_questions,omitempty"`
	ActionMinutes int  `json:"action_minutes,omitempty"`
}

var sentenceRequest = regexp.MustCompile(`^(?:(?:请|麻烦|现在|这次|接下来|再|只|就|你|给我)\s*)*(?:用|给我)?([一二两三四五六1-6])句(?:话)?`)
var noQuestionRequest = regexp.MustCompile(`^(?:(?:请|现在|这次|先)\s*)*(?:不要|不用|别|不必|无需)(?:再|继续|向我)?(?:追问|问我|提问|问问题|问$)`)
var actionTimeRequest = regexp.MustCompile(`(?:不超过|最多|至多|只有|只用)([一二两三四五六七八九十1-9]|10)分钟`)
var actionRequest = regexp.MustCompile(`^(?:我(?:愿意|想|可以)(?:试|做)|(?:请)?(?:给我|安排|设计)|(?:请)?(?:试|做)一个)`)

func smallNumber(s string) int {
	if s == "10" {
		return 10
	}
	for i, v := range []string{"一", "二", "三", "四", "五", "六", "七", "八", "九", "十"} {
		if s == v {
			return i + 1
		}
	}
	if s == "两" {
		return 2
	}
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		return int(s[0] - '0')
	}
	return 0
}

func CurrentResponseConstraints(user string) ResponseConstraints {
	// Replace quotations including their punctuation, so a request embedded in
	// quoted source material cannot become a current request.
	var plain strings.Builder
	var close rune
	for _, r := range user {
		if close != 0 {
			if r == close {
				close = 0
				plain.WriteRune('；')
			}
			continue
		}
		switch r {
		case '“':
			close = '”'
		case '「':
			close = '」'
		case '『':
			close = '』'
		case '‘':
			close = '’'
		case '"', '`':
			close = r
		default:
			plain.WriteRune(r)
		}
		if close != 0 {
			plain.WriteString("引用")
		}
	}
	var out ResponseConstraints
	for _, part := range strings.FieldsFunc(plain.String(), func(r rune) bool { return strings.ContainsRune("，,。！？!?；;\n", r) }) {
		part = strings.TrimSpace(part)
		if m := sentenceRequest.FindStringSubmatch(part); m != nil {
			out.Sentences = smallNumber(m[1])
		}
		out.NoQuestions = out.NoQuestions || noQuestionRequest.MatchString(part)
		// Only an explicit request to try/do an action gives a time cap. A
		// description of someone else's schedule must not constrain this turn.
		if actionRequest.MatchString(part) && (strings.Contains(part, "行动") || strings.Contains(part, "练习")) {
			if m := actionTimeRequest.FindStringSubmatch(part); m != nil {
				out.ActionMinutes = smallNumber(m[1])
			}
		}
	}
	return out
}

// SentenceCount declares the same punctuation rule used by comparison checks.
// Decimal points and semicolons do not end a sentence; an unpunctuated suffix
// is counted. Semantic completeness remains a separate review item.
func SentenceCount(s string) int {
	n, pending := 0, false
	for _, r := range s {
		if strings.ContainsRune("。！？!?", r) {
			if pending {
				n++
				pending = false
			}
		} else if !unicode.IsSpace(r) && !strings.ContainsRune("”’」』\"", r) {
			pending = true
		}
	}
	if pending {
		n++
	}
	return n
}

func (c ResponseConstraints) Check(text string) []string {
	var issues []string
	if c.Sentences > 0 && SentenceCount(text) != c.Sentences {
		issues = append(issues, "sentence_count")
	}
	if c.NoQuestions && strings.ContainsAny(text, "?？") {
		issues = append(issues, "explicit_no_questions")
	}
	return issues
}
