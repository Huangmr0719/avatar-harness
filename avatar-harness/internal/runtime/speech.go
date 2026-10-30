package runtime

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
)

type Segmenter struct {
	buffer  []rune
	emitted int
}

func (s *Segmenter) Feed(text string, final bool) []string {
	s.buffer = append(s.buffer, []rune(text)...)
	out := []string{}
	start := 0
	for i, ch := range s.buffer {
		numericMark := strings.ContainsRune(".,:，：", ch) && i > 0 && unicode.IsDigit(s.buffer[i-1])
		if !final && i == len(s.buffer)-1 && (ch == '.' || numericMark) {
			break
		}
		length := i - start + 1
		numeric := numericMark && i+1 < len(s.buffer) && unicode.IsDigit(s.buffer[i+1])
		hard := strings.ContainsRune("。！？!?\n", ch) || (ch == '.' && !numeric)
		early := s.emitted < 2 && !numeric && strings.ContainsRune("，,；;：:", ch) && speechUnits(s.buffer[start:i+1]) >= 8
		limit := 96
		if s.emitted == 0 {
			limit = 24
		} else if s.emitted == 1 {
			limit = 48
		}
		bounded := length >= limit && speechBoundary(s.buffer, i+1) && speechUnits(s.buffer[start:i+1]) >= 8
		if hard || early || bounded {
			piece := strings.TrimSpace(string(s.buffer[start : i+1]))
			if piece != "" {
				out = append(out, piece)
				s.emitted++
			}
			start = i + 1
		}
	}
	s.buffer = s.buffer[start:]
	if final && len(s.buffer) > 0 {
		if piece := strings.TrimSpace(string(s.buffer)); piece != "" {
			out = append(out, piece)
			s.emitted++
		}
		s.buffer = nil
	}
	return out
}

// Flush advances a stalled text stream without cutting an unfinished Latin
// word or a number. Very short fragments still wait for more text/final flush.
func (s *Segmenter) Flush() []string {
	minimum := 12
	if s.emitted == 0 {
		minimum = 8
	}
	for cut := len(s.buffer); cut > 0; cut-- {
		if speechBoundary(s.buffer, cut) && speechUnits(s.buffer[:cut]) >= minimum {
			piece := strings.TrimSpace(string(s.buffer[:cut]))
			s.buffer = s.buffer[cut:]
			s.emitted++
			return []string{piece}
		}
	}
	return nil
}

func cjk(ch rune) bool {
	return unicode.Is(unicode.Han, ch) || unicode.Is(unicode.Hiragana, ch) || unicode.Is(unicode.Katakana, ch) || unicode.Is(unicode.Hangul, ch)
}

func speechBoundary(text []rune, cut int) bool {
	ch := text[cut-1]
	return unicode.IsSpace(ch) || cjk(ch)
}

// One CJK character or half a whitespace-delimited word is a small speech
// unit. This avoids applying an English word-count threshold to Chinese.
func speechUnits(text []rune) int {
	units, word := 0, false
	for _, ch := range text {
		if cjk(ch) {
			units++
			word = false
		} else if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			if !word {
				units += 2
			}
			word = true
		} else {
			word = false
		}
	}
	return units
}

type speechJob struct {
	index   int
	text    string
	queued  time.Time
	packets chan map[string]any
}

type speechText struct {
	text  string
	final bool
	done  chan error
}

type Speech struct {
	ctx        context.Context
	cancel     context.CancelFunc
	config     *config.Config
	character  config.Character
	bus        *provider.Bus
	metadata   map[string]any
	emit       func(string, map[string]any)
	split      Segmenter
	text       chan speechText
	jobs       chan speechJob
	order      chan chan map[string]any
	done       chan struct{}
	stages     sync.WaitGroup
	feedMu     sync.Mutex
	disabled   bool
	closed     bool
	active     atomic.Int32
	failed     atomic.Bool
	expression atomic.Pointer[Expression]
}

func NewSpeech(parent context.Context, c *config.Config, ch config.Character, b *provider.Bus, metadata map[string]any, emit func(string, map[string]any)) *Speech {
	ctx, cancel := context.WithCancel(parent)
	s := &Speech{ctx: ctx, cancel: cancel, config: c, character: ch, bus: b, metadata: metadata, emit: emit, text: make(chan speechText), jobs: make(chan speechJob, c.Limits.TTSConcurrency), order: make(chan chan map[string]any, 2*c.Limits.TTSConcurrency), done: make(chan struct{}), disabled: c.Providers["tts"].Kind == "deferred"}
	if s.disabled {
		close(s.done)
		emit("pipeline.deferred", map[string]any{"capability": "tts", "message": "语音合成尚未接入，本轮仅输出日志文字。"})
		return s
	}
	for i := 0; i < c.Limits.TTSConcurrency; i++ {
		stream := b.NewTTSStream(ctx, map[string]any{"voice": ch.Voice, "expression_intent": map[string]any{"pad": ch.PAD}}, metadata)
		s.stages.Add(1)
		go s.worker(stream)
	}
	s.stages.Add(2)
	go s.segment()
	go s.dispatch()
	go func() { s.stages.Wait(); close(s.done) }()
	return s
}

func (s *Speech) SetExpression(expression Expression) { s.expression.Store(&expression) }

func (s *Speech) Feed(text string, final bool) error {
	if s.disabled {
		return nil
	}
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return fmt.Errorf("语音文本流已结束")
	}
	s.closed = final
	input := speechText{text: text, final: final, done: make(chan error, 1)}
	select {
	case s.text <- input:
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
	select {
	case err := <-input.done:
		return err
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// One stage owns the segmenter and its timer. A maximum wait is measured from
// the first buffered text, not reset by every token. Channels bound look-ahead;
// a slow consumer backpressures the producer without buffering whole answers.
func (s *Speech) segment() {
	defer s.stages.Done()
	defer close(s.jobs)
	defer close(s.order)
	firstWait := time.Duration(s.config.Limits.TTSFirstChunkMS) * time.Millisecond
	laterWait := time.Duration(s.config.Limits.TTSChunkMS) * time.Millisecond
	if firstWait <= 0 {
		firstWait = 180 * time.Millisecond
	}
	if laterWait <= 0 {
		laterWait = 400 * time.Millisecond
	}
	timer := time.NewTimer(firstWait)
	timer.Stop()
	defer timer.Stop()
	var tick <-chan time.Time
	var bufferedAt time.Time
	index := 0
	enqueue := func(pieces []string) error {
		for _, piece := range pieces {
			job := speechJob{index: index, text: piece, queued: time.Now(), packets: make(chan map[string]any, 4)}
			select {
			case s.order <- job.packets:
			case <-s.ctx.Done():
				return s.ctx.Err()
			}
			select {
			case s.jobs <- job:
				index++
			case <-s.ctx.Done():
				return s.ctx.Err()
			}
		}
		return nil
	}
	arm := func() {
		timer.Stop()
		tick = nil
		if len(s.split.buffer) == 0 {
			bufferedAt = time.Time{}
			return
		}
		if bufferedAt.IsZero() {
			bufferedAt = time.Now()
		}
		wait := laterWait
		if s.split.emitted == 0 {
			wait = firstWait
		}
		timer.Reset(max(0, wait-time.Since(bufferedAt)))
		tick = timer.C
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case input := <-s.text:
			before := s.split.emitted
			err := enqueue(s.split.Feed(input.text, input.final))
			input.done <- err
			if err != nil || input.final {
				return
			}
			if s.split.emitted != before {
				bufferedAt = time.Time{}
			}
			arm()
		case <-tick:
			tick = nil
			pieces := s.split.Flush()
			if len(pieces) == 0 {
				// No safe, sufficiently long boundary yet. Try again on new
				// text rather than spinning a timer on the same tiny suffix.
				continue
			}
			if enqueue(pieces) != nil {
				return
			}
			bufferedAt = time.Time{}
			arm()
		}
	}
}

func (s *Speech) worker(stream *provider.TTSStream) {
	defer s.stages.Done()
	defer stream.Close()
	for {
		select {
		case <-s.ctx.Done():
			return
		case job, ok := <-s.jobs:
			if !ok {
				return
			}
			s.generate(stream, job)
		}
	}
}
func (s *Speech) generate(stream *provider.TTSStream, job speechJob) {
	defer close(job.packets)
	s.active.Add(1)
	defer func() {
		if s.active.Add(-1) == 0 {
			state := "configured"
			if s.failed.Load() {
				state = "error"
			}
			s.emit("module.status", map[string]any{"module": "tts", "state": state})
		}
	}()
	send := func(packet map[string]any) error {
		select {
		case job.packets <- packet:
			return nil
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
	s.emit("module.status", map[string]any{"module": "tts", "state": "running"})
	start := time.Now()
	s.emit("pipeline.timing", map[string]any{"stage": "tts.segment", "segment": job.index, "characters": len([]rune(job.text)), "queue_ms": float64(start.Sub(job.queued).Microseconds()) / 1000})
	first := true
	intent := map[string]any{"style": "neutral", "intensity": .6, "pad": s.character.PAD}
	if expression := s.expression.Load(); expression != nil {
		intent["style"], intent["intensity"] = expression.Style, expression.Intensity
	}
	err := stream.StreamWithInput(job.text, map[string]any{"expression_intent": intent}, func(e map[string]any) error {
		if status := provider.String(e["status"]); status != "" && status != "ok" {
			return fmt.Errorf("TTS: %s", provider.String(e["error"]))
		}
		if e["type"] == "progress" {
			data := copyMap(e)
			delete(data, "type")
			data["segment"] = job.index
			s.emit("pipeline.timing", data)
			return nil
		}
		if e["type"] == "done" {
			return nil
		}
		packet, _ := e["output"].(map[string]any)
		if packet == nil {
			packet, _ = e["data"].(map[string]any)
		}
		if packet == nil {
			packet = e
		}
		if provider.String(packet["audio_base64"]) == "" {
			return fmt.Errorf("TTS 缺少 audio_base64")
		}
		if first {
			first = false
			s.emit("pipeline.timing", map[string]any{"stage": "tts.first_audio", "segment": job.index, "elapsed_ms": float64(time.Since(start).Microseconds()) / 1000})
		}
		return send(packet)
	})
	if err != nil && s.ctx.Err() == nil {
		s.failed.Store(true)
		_ = send(map[string]any{"error": err.Error()})
		s.emit("module.status", map[string]any{"module": "tts", "state": "error", "message": err.Error()})
	}
}
func (s *Speech) dispatch() {
	defer s.stages.Done()
	pts := 0.0
	for {
		var packets chan map[string]any
		select {
		case <-s.ctx.Done():
			return
		case next, ok := <-s.order:
			if !ok {
				return
			}
			packets = next
		}
		for {
			var packet map[string]any
			select {
			case <-s.ctx.Done():
				return
			case next, ok := <-packets:
				if !ok {
					packets = nil
				} else {
					packet = next
				}
			}
			if packets == nil {
				break
			}
			if packet["error"] != nil {
				s.emit("pipeline.error", map[string]any{"capability": "tts", "message": packet["error"]})
				continue
			}
			duration, err := AudioDuration(packet)
			if err != nil {
				s.emit("pipeline.error", map[string]any{"capability": "tts", "message": err.Error()})
				continue
			}
			audio := copyMap(packet)
			audio["pts_ms"] = pts
			audio["duration_ms"] = duration
			audio["mode"] = "audio"
			if s.config.Providers["avatar"].Kind != "deferred" && s.character.Renderer.Driver != "remote" {
				s.emit("module.status", map[string]any{"module": "avatar", "state": "running"})
				result := s.bus.Invoke(s.ctx, "avatar", map[string]any{"operation": "animate", "audio": audio, "character_id": s.character.ID, "asset_path": s.character.AssetPath, "expression_intent": map[string]any{"pad": s.character.PAD}}, s.metadata)
				if result.Status == "ok" {
					data := copyMap(result.Output)
					data["pts_ms"] = pts
					s.emit("pipeline.avatar", data)
					audio["mode"] = "audio_video_pending_client"
					s.emit("module.status", map[string]any{"module": "avatar", "state": "configured"})
				} else {
					s.emit("pipeline.error", map[string]any{"capability": "avatar", "message": result.Error})
					s.emit("module.status", map[string]any{"module": "avatar", "state": "error", "message": result.Error})
				}
			}
			s.emit("pipeline.audio", audio)
			pts += duration
		}
	}
}
func (s *Speech) Finish() error {
	if s.disabled {
		return nil
	}
	if err := s.Feed("", true); err != nil {
		return err
	}
	select {
	case <-s.done:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
func (s *Speech) Stop() { s.cancel(); <-s.done }
func AudioDuration(audio map[string]any) (float64, error) {
	raw, err := base64.StdEncoding.DecodeString(provider.String(audio["audio_base64"]))
	if err != nil || len(raw) == 0 {
		return 0, fmt.Errorf("无效的音频 Base64")
	}
	format := provider.String(audio["format"])
	if format == "wav" {
		if len(raw) < 12 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
			return 0, fmt.Errorf("无效的 WAV")
		}
		rate := uint32(0)
		size := uint32(0)
		for offset := 12; offset+8 <= len(raw); {
			n := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
			if n < 0 || n > len(raw)-offset-8 {
				return 0, fmt.Errorf("WAV 数据长度错误")
			}
			switch string(raw[offset : offset+4]) {
			case "fmt ":
				if n >= 16 {
					rate = binary.LittleEndian.Uint32(raw[offset+16 : offset+20])
				}
			case "data":
				size = uint32(n)
			}
			offset += 8 + n + n%2
		}
		if rate == 0 || size == 0 {
			return 0, fmt.Errorf("WAV 缺少 fmt/data")
		}
		return float64(size) * 1000 / float64(rate), nil
	}
	if format != "" && format != "pcm_s16le" {
		return 0, fmt.Errorf("音频只支持 pcm_s16le 或 WAV")
	}
	rate := provider.Number(audio["sample_rate"])
	if rate == 0 {
		rate = 24000
	}
	channels := int(provider.Number(audio["channels"]))
	if channels == 0 {
		channels = 1
	}
	if rate < 8000 || rate > 192000 || (channels != 1 && channels != 2) || len(raw)%(channels*2) != 0 {
		return 0, fmt.Errorf("PCM 采样率、声道或长度错误")
	}
	return float64(len(raw)) * 1000 / (rate * float64(channels) * 2), nil
}
