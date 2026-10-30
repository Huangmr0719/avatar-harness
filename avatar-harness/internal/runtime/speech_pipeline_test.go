package runtime

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"github.com/gorilla/websocket"
)

type ttsPipelineFixture struct {
	connections atomic.Int32
	commits     atomic.Int32
	configured  chan struct{}
}

// This peer accepts both the old finish-per-segment protocol and a reusable
// commit-mode session. The optional delay represents connection preparation,
// not a model or cloud latency estimate.
func newTTSPipelineFixture(t testing.TB, setupDelay time.Duration) (*config.Config, *provider.Bus, *ttsPipelineFixture) {
	t.Helper()
	t.Setenv("TTS_PIPELINE_TEST_KEY", "local-protocol-fixture")
	fixture := &ttsPipelineFixture{configured: make(chan struct{}, 4)}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		fixture.connections.Add(1)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		text := ""
		for {
			var event map[string]any
			if conn.ReadJSON(&event) != nil {
				return
			}
			switch event["type"] {
			case "session.update":
				time.Sleep(setupDelay)
				_ = conn.WriteJSON(map[string]any{"type": "session.updated"})
				select {
				case fixture.configured <- struct{}{}:
				default:
				}
			case "input_text_buffer.append":
				text += provider.String(event["text"])
			case "input_text_buffer.commit":
				id := fmt.Sprint(fixture.commits.Add(1))
				raw := make([]byte, 480)
				for i := 0; i < len(raw); i += 2 {
					binary.LittleEndian.PutUint16(raw[i:], uint16([]rune(text)[0]))
				}
				_ = conn.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
				_ = conn.WriteJSON(map[string]any{"type": "response.audio.delta", "response_id": id, "delta": base64.StdEncoding.EncodeToString(raw)})
				_ = conn.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}})
				text = ""
			case "session.finish":
				_ = conn.WriteJSON(map[string]any{"type": "session.finished"})
				return
			}
		}
	}))
	t.Cleanup(remote.Close)
	c := config.Default()
	c.Limits.TTSConcurrency = 1
	c.Providers["tts"] = config.Provider{Kind: "bailian", BaseURL: "ws" + strings.TrimPrefix(remote.URL, "http"), Model: "tts-fixture", APIKeyEnv: "TTS_PIPELINE_TEST_KEY", Streaming: true, TimeoutMS: 2000}
	b, err := provider.New(&c, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &c, b, fixture
}

func TestSpeechPreparesTTSBeforeTextAndReusesConnection(t *testing.T) {
	c, b, fixture := newTTSPipelineFixture(t, 0)
	var packets []map[string]any
	s := NewSpeech(context.Background(), c, c.Characters[0], b, nil, func(kind string, data map[string]any) {
		if kind == "pipeline.audio" {
			packets = append(packets, data)
		}
	})
	defer s.Stop()
	select {
	case <-fixture.configured:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("TTS connection preparation waited for the first text segment")
	}
	if err := s.Feed("甲句。乙句。丙句。", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	if fixture.connections.Load() != 1 || fixture.commits.Load() != 3 {
		t.Fatalf("connections=%d commits=%d; expected three responses on one connection", fixture.connections.Load(), fixture.commits.Load())
	}
	if len(packets) != 3 {
		t.Fatal(packets)
	}
	for i, expected := range []rune{'甲', '乙', '丙'} {
		raw, _ := base64.StdEncoding.DecodeString(provider.String(packets[i]["audio_base64"]))
		if binary.LittleEndian.Uint16(raw) != uint16(expected) || packets[i]["pts_ms"] != float64(i*10) {
			t.Fatal("audio responses were mixed or lost their sample clock", packets)
		}
	}
}

func TestSpeechFlushesFirstPageWhileTextProducerIsPaused(t *testing.T) {
	c := config.Default()
	c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "page-tts", TimeoutMS: 1000}
	b, _ := provider.New(&c, nil)
	spoken := make(chan string, 4)
	b.Register("page-tts", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		spoken <- provider.String(req.Input["text"])
		return provider.OK(map[string]any{"audio_base64": "AQABAA==", "format": "pcm_s16le", "sample_rate": 24000, "channels": 1}), nil
	})
	s := NewSpeech(context.Background(), &c, c.Characters[0], b, nil, func(string, map[string]any) {})
	defer s.Stop()
	opening, rest := "光合作用把光能转换成化学能", "供植物生长。"
	if err := s.Feed(opening, false); err != nil {
		t.Fatal(err)
	}
	var first string
	select {
	case first = <-spoken:
	case <-time.After(600 * time.Millisecond):
		t.Fatal("first page waited for punctuation or the full response")
	}
	if first != opening {
		t.Fatal("unexpected first page", first)
	}
	if err := s.Feed(rest, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	if got := first + <-spoken; got != opening+rest {
		t.Fatal("timed flush duplicated or lost text", got)
	}
}

func TestSegmenterUsesShortOpeningPagesWithoutLosingText(t *testing.T) {
	var s Segmenter
	text := strings.Repeat("这是一段连续的中文正文", 12)
	parts := s.Feed(text, false)
	if len(parts) == 0 || len([]rune(parts[0])) > 24 {
		t.Fatal("opening page retained the long-answer limit", parts)
	}
	parts = append(parts, s.Feed("", true)...)
	if strings.Join(parts, "") != text {
		t.Fatal("segmentation changed the answer")
	}
}

func TestSpeechConcurrentBailianLanesPreserveSegmentOwnership(t *testing.T) {
	c, b, fixture := newTTSPipelineFixture(t, 0)
	c.Limits.TTSConcurrency = 2
	var packets []map[string]any
	s := NewSpeech(context.Background(), c, c.Characters[0], b, nil, func(kind string, data map[string]any) {
		if kind == "pipeline.audio" {
			packets = append(packets, data)
		}
	})
	defer s.Stop()
	for range c.Limits.TTSConcurrency {
		select {
		case <-fixture.configured:
		case <-time.After(time.Second):
			t.Fatal("synthesis lanes did not prepare independently")
		}
	}
	if err := s.Feed("甲句。乙句。丙句。丁句。戊句。", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	if fixture.connections.Load() != 2 || fixture.commits.Load() != 5 || len(packets) != 5 {
		t.Fatal(fixture.connections.Load(), fixture.commits.Load(), len(packets))
	}
	for i, expected := range []rune{'甲', '乙', '丙', '丁', '戊'} {
		raw, _ := base64.StdEncoding.DecodeString(provider.String(packets[i]["audio_base64"]))
		if binary.LittleEndian.Uint16(raw) != uint16(expected) || packets[i]["pts_ms"] != float64(i*10) {
			t.Fatal("concurrent responses leaked into another segment", packets)
		}
	}
}

func TestSegmenterTimedFlushKeepsIncompleteWordsAndNumbers(t *testing.T) {
	var number Segmenter
	prefix := "这里测量出来的真实数值为"
	if parts := number.Feed(prefix+"3.", false); len(parts) != 0 {
		t.Fatal(parts)
	}
	if got := strings.Join(number.Flush(), ""); got != prefix {
		t.Fatal("timed flush split a pending decimal", got)
	}
	if got := strings.Join(number.Feed("14米。", false), ""); got != "3.14米。" {
		t.Fatal("decimal was lost across a timed flush", got)
	}
	var words Segmenter
	_ = words.Feed("One two red blue incom", false)
	if got := strings.Join(words.Flush(), ""); got != "One two red blue" {
		t.Fatal("timed flush cut an unfinished word", got)
	}
	if got := strings.Join(words.Feed("plete.", true), ""); got != "incomplete." {
		t.Fatal(got)
	}
	for _, chunks := range [][]string{{"现在的北京时间为12:", "34。"}, {"这里的实际统计结果为1,", "234个。"}} {
		var s Segmenter
		parts := s.Feed(chunks[0], false)
		if len(parts) != 0 {
			t.Fatal("numeric punctuation cut the first segment", parts)
		}
		parts = append(parts, s.Feed(chunks[1], true)...)
		if strings.Join(parts, "") != strings.Join(chunks, "") || len(parts) != 1 {
			t.Fatal("number/time split across segments", parts)
		}
	}
}

func TestSpeechChunkBudgetDoesNotSlideWithEveryToken(t *testing.T) {
	c := config.Default()
	c.Limits.TTSFirstChunkMS = 100
	c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "budget-tts", TimeoutMS: 1000}
	b, _ := provider.New(&c, nil)
	spoken := make(chan string, 4)
	b.Register("budget-tts", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		spoken <- provider.String(req.Input["text"])
		return provider.OK(map[string]any{"audio_base64": "AQABAA==", "format": "pcm_s16le", "sample_rate": 24000, "channels": 1}), nil
	})
	s := NewSpeech(context.Background(), &c, c.Characters[0], b, nil, func(string, map[string]any) {})
	defer s.Stop()
	if err := s.Feed("One two red blue ", false); err != nil {
		t.Fatal(err)
	}
	tokens := time.NewTicker(10 * time.Millisecond)
	defer tokens.Stop()
	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()
	for {
		select {
		case first := <-spoken:
			if first != "One two red blue" {
				t.Fatal("unfinished word entered TTS", first)
			}
			if err := s.Finish(); err != nil {
				t.Fatal(err)
			}
			return
		case <-tokens.C:
			// No new safe boundary: only the timer can release the prefix.
			if err := s.Feed("x", false); err != nil {
				t.Fatal(err)
			}
		case <-deadline.C:
			t.Fatal("new tokens kept postponing the first-page deadline")
		}
	}
}

func TestSpeechBackpressureCanStillBeCancelled(t *testing.T) {
	c := config.Default()
	c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "blocked-page", TimeoutMS: 2000}
	b, _ := provider.New(&c, nil)
	started := make(chan struct{}, c.Limits.TTSConcurrency)
	b.Register("blocked-page", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		select {
		case started <- struct{}{}:
		case <-ctx.Done():
		}
		<-ctx.Done()
		return provider.Result{}, ctx.Err()
	})
	s := NewSpeech(context.Background(), &c, c.Characters[0], b, nil, func(string, map[string]any) {})
	defer s.Stop()
	feeding := make(chan error, 1)
	go func() { feeding <- s.Feed(strings.Repeat("测试分段。", 100), false) }()
	<-started
	s.Stop()
	select {
	case err := <-feeding:
		if err == nil {
			t.Fatal("blocked producer did not observe cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation deadlocked the bounded pipeline")
	}
}

func BenchmarkSpeechPipelineFirstAudio(b *testing.B) {
	c, bus, fixture := newTTSPipelineFixture(b, 40*time.Millisecond)
	firstMS := 0.0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		first := true
		s := NewSpeech(context.Background(), c, c.Characters[0], bus, nil, func(kind string, data map[string]any) {
			if kind == "pipeline.audio" && first {
				firstMS += float64(time.Since(start).Microseconds()) / 1000
				first = false
			}
		})
		// Scripted LLM first-phrase wait. TTS preparation should overlap it.
		time.Sleep(120 * time.Millisecond)
		if err := s.Feed("甲句。乙句。丙句。", false); err != nil {
			b.Fatal(err)
		}
		if err := s.Finish(); err != nil {
			b.Fatal(err)
		}
		s.Stop()
	}
	b.StopTimer()
	b.ReportMetric(firstMS/float64(b.N), "first_audio_ms/op")
	b.ReportMetric(float64(fixture.connections.Load())/float64(b.N), "ws_connections/op")
}
