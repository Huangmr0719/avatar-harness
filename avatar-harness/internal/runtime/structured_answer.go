package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const expressionPrompt = `
数字人回复使用紧凑 JSON，键顺序固定：{"emotion":"neutral","intensity":0.6,"speech":"<speak/>可播报正文"}。
emotion 使用基础七类：neutral（中性）、happy（快乐）、sad（悲伤）、angry（愤怒）、fearful（恐惧）、surprised（惊讶）、disgusted（厌恶）。选择数字人回应时适宜表达的情感，结合对话内容与用户情绪；安慰时可低强度悲伤或温和中性，不要机械模仿用户，也不要无故对用户表达愤怒或厌恶。intensity 在 0 到 1 之间，日常对话适度；同一情感与强度同时用于脸部表情和语音语气。speech 必须以 <speak/> 开头，然后直接回答用户。不要代码围栏、解释 JSON 或额外字段，不在表情前输出思考过程。
需要外部工具时直接输出原生 tool_calls，不输出 JSON 或前言；拿到工具结果后的最终回答再用上述 JSON。表情属于同一条回复，不需要单独调用工具。`

type Expression struct {
	Style      string  `json:"style"`
	Intensity  float64 `json:"intensity"`
	DurationMS int     `json:"duration_ms"`
}

// StructuredAnswer parses the short control prefix, then decodes the speech
// string incrementally. A complete JSON response is never required for TTS.
type StructuredAnswer struct {
	Enabled bool
	mode    string
	raw     strings.Builder
	buffer  string
	started bool
	ended   bool
}

func (s *StructuredAnswer) Feed(chunk string, final bool) (string, *Expression, error) {
	if !s.Enabled || s.mode == "plain" {
		return chunk, nil, nil
	}
	s.raw.WriteString(chunk)
	s.buffer += chunk
	var expression *Expression
	if s.mode == "" {
		trimmed := strings.TrimSpace(s.buffer)
		if trimmed == "" && !final {
			return "", nil, nil
		}
		if !strings.HasPrefix(trimmed, "{") {
			s.mode = "plain" // Legacy providers can still return <speak/> text.
			text := s.buffer
			s.buffer = ""
			return text, nil, nil
		}
		s.mode = "json"
	}
	if !s.started {
		position, control, err := expressionHeader(s.buffer)
		if position < 0 {
			if !final && len(s.buffer) <= 4096 && (err == nil || err == io.EOF || err == io.ErrUnexpectedEOF || err.Error() == "unexpected end of JSON input") {
				return "", nil, nil
			}
			return "", nil, fmt.Errorf("数字人结构化回复头无效或过长: %v", err)
		}
		if position > 4096 {
			return "", nil, fmt.Errorf("数字人结构化回复头过长")
		}
		s.started = true
		expression = control
		s.buffer = s.buffer[position:]
	}
	text, err := s.decodeSpeech(final)
	if err != nil {
		return "", nil, err
	}
	if final {
		var response struct {
			Speech *string `json:"speech"`
		}
		if err := json.Unmarshal([]byte(s.raw.String()), &response); err != nil || response.Speech == nil || !s.ended {
			return "", nil, fmt.Errorf("数字人结构化回复不完整")
		}
	}
	return text, expression, nil
}

// Return the first character inside speech, after consuming complete metadata.
func expressionHeader(input string) (int, *Expression, error) {
	d := json.NewDecoder(strings.NewReader(input))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return -1, nil, err
	}
	style, intensity := "", 0.7
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return -1, nil, err
		}
		if key == "speech" {
			position := int(d.InputOffset())
			for position < len(input) && strings.ContainsRune(" \r\n\t", rune(input[position])) {
				position++
			}
			if position == len(input) {
				return -1, nil, io.EOF
			}
			if input[position] != ':' {
				return -1, nil, fmt.Errorf("speech 缺少冒号")
			}
			position++
			for position < len(input) && strings.ContainsRune(" \r\n\t", rune(input[position])) {
				position++
			}
			if position == len(input) {
				return -1, nil, io.EOF
			}
			if input[position] != '"' {
				return -1, nil, fmt.Errorf("speech 必须为字符串")
			}
			var expression *Expression
			if validExpressionStyle(style) && !math.IsNaN(intensity) && !math.IsInf(intensity, 0) && intensity >= 0 && intensity <= 1 {
				expression = &Expression{style, intensity, 8000}
			}
			return position + 1, expression, nil
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return -1, nil, err
		}
		switch key {
		case "emotion":
			_ = json.Unmarshal(value, &style)
		case "intensity":
			if json.Unmarshal(value, &intensity) != nil {
				intensity = -1
			}
		}
	}
	return -1, nil, io.EOF
}

func validExpressionStyle(style string) bool {
	switch style {
	case "neutral", "happy", "sad", "angry", "fearful", "surprised", "disgusted":
		return true
	case "natural", "focused", "concerned": // Compatibility with existing providers and sessions.
		return true
	}
	return false
}

func (s *StructuredAnswer) decodeSpeech(final bool) (string, error) {
	if s.ended {
		s.buffer = ""
		return "", nil
	}
	var out strings.Builder
	i := 0
	for i < len(s.buffer) {
		c := s.buffer[i]
		if c == '"' {
			s.ended = true
			i++
			break
		}
		if c < 0x20 {
			return "", fmt.Errorf("speech 含未转义控制字符")
		}
		if c != '\\' {
			if !utf8.FullRuneInString(s.buffer[i:]) {
				break
			}
			_, n := utf8.DecodeRuneInString(s.buffer[i:])
			out.WriteString(s.buffer[i : i+n])
			i += n
			continue
		}
		if len(s.buffer)-i < 2 {
			break
		}
		escape := s.buffer[i+1]
		if escape == 'u' {
			if len(s.buffer)-i < 6 {
				break
			}
			code, err := strconv.ParseUint(s.buffer[i+2:i+6], 16, 16)
			if err != nil {
				return "", fmt.Errorf("speech Unicode 转义无效")
			}
			r := rune(code)
			if r >= 0xD800 && r <= 0xDBFF {
				if len(s.buffer)-i < 12 {
					break
				}
				if s.buffer[i+6:i+8] != "\\u" {
					return "", fmt.Errorf("speech Unicode 代理对无效")
				}
				low, err := strconv.ParseUint(s.buffer[i+8:i+12], 16, 16)
				if err != nil || low < 0xDC00 || low > 0xDFFF {
					return "", fmt.Errorf("speech Unicode 代理对无效")
				}
				r = utf16.DecodeRune(r, rune(low))
				i += 6
			}
			out.WriteRune(r)
			i += 6
			continue
		}
		escapes := map[byte]byte{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}
		value, ok := escapes[escape]
		if !ok {
			return "", fmt.Errorf("speech 转义无效")
		}
		out.WriteByte(value)
		i += 2
	}
	s.buffer = s.buffer[i:]
	if final && !s.ended {
		return "", fmt.Errorf("speech 字符串未结束")
	}
	return out.String(), nil
}
