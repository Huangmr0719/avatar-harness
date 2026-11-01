package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

func TestPerceptionFreshnessConsentAndTurnIsolation(t *testing.T) {
	c := config.Default()
	c.Tools = nil
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "capture", TimeoutMS: 1000}
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	var captured string
	b.Register("capture", func(ctx context.Context, q provider.Request, emit provider.Emit) (provider.Result, error) {
		raw, _ := json.Marshal(q.Input["messages"])
		captured = string(raw)
		return provider.OK(map[string]any{"content": "已收到。"}), nil
	})
	r := New(&c, s, b)
	defer r.Shutdown()
	sid := s.Create(c.Characters[0].ID).ID
	token := r.BeginPerception(sid)
	face := map[string]any{"face_detected": true, "au_intensities": map[string]any{"AU12": 2.4}}
	if !r.ObserveFace(sid, token, 1, time.Now(), face) {
		t.Fatal("fresh frame rejected")
	}
	if r.ObserveFace(sid, token, 1, time.Now(), face) || r.ObserveFace(sid, token, 2, time.Now().Add(-5*time.Second), face) {
		t.Fatal("stale frame accepted")
	}
	turn, _ := r.StartVoice(sid, "我很好", "input-one", time.Now(), map[string]any{"emotion": "sad", "language": "zh"})
	<-turn.Done
	if !strings.Contains(captured, "AU12") || !strings.Contains(captured, "sad") || !strings.Contains(captured, "user_perception") {
		t.Fatal("observations did not reach LLM", captured)
	}
	for _, e := range s.Events(sid, 0) {
		raw, _ := json.Marshal(e.Data)
		if strings.Contains(string(raw), "AU12") {
			t.Fatal("ephemeral face data persisted")
		}
	}
	r.EndPerception(sid, token)
	if r.ObserveFace(sid, token, 3, time.Now(), face) {
		t.Fatal("late frame revived stopped camera")
	}
	next, _ := r.Start(sid, "下一轮", "text", false, nil)
	<-next.Done
	if strings.Contains(captured, "user_perception") || strings.Contains(captured, "sad") {
		t.Fatal("old observations leaked to new turn", captured)
	}
	fresh := r.BeginPerception(sid)
	r.EndPerception(sid, token)
	if !r.PerceptionActive(sid, fresh) {
		t.Fatal("old stop cleared new stream")
	}
}

func emotionFixture(label string) map[string]any {
	scores := map[string]any{}
	for _, k := range []string{"neutral", "happy", "sad", "angry", "surprised", "fearful", "disgusted", "contempt"} {
		scores[k] = .1 / 7
	}
	scores[label] = .9
	return map[string]any{"face_detected": true, "source": "emotieffnet_b0+libreface_au", "expression": map[string]any{"label": label, "scores": scores}, "au_intensities": map[string]any{"AU12": 2.0}}
}
func TestUtteranceSummaryExcludesWaitingFacesAndRetainsEndingTrend(t *testing.T) {
	c := config.Default()
	r := New(&c, nil, nil)
	now := time.Now()
	start := now.Add(-12 * time.Second)
	end := now.Add(-2 * time.Second)
	p := &perceptionSession{token: "active"}
	r.perception["s"] = p
	p.samples = append(p.samples, faceSample{start.Add(-time.Second), emotionFixture("angry")})
	for _, offset := range []time.Duration{1, 2, 3, 4, 5} {
		p.samples = append(p.samples, faceSample{start.Add(offset * time.Second), emotionFixture("happy")})
	}
	for _, offset := range []time.Duration{9, 10} {
		p.samples = append(p.samples, faceSample{start.Add(offset * time.Second), emotionFixture("sad")})
	}
	p.samples = append(p.samples, faceSample{end.Add(time.Second), emotionFixture("angry")})
	signals := r.signalsLocked("s", map[string]any{"_speech_started_at": start, "emotion": "neutral"}, now, end)
	face := signals["face"].(map[string]any)
	ending := face["ending"].(map[string]any)
	if face["sample_count"] != 7 || face["dominant_label"] != "happy" || ending["dominant_label"] != "sad" {
		t.Fatal("wrong utterance selection", face)
	}
	if face["window"].(map[string]any)["kind"] != "utterance" {
		t.Fatal("missing temporal provenance", face)
	}
	// ASR latency and newer waiting frames must not move the selected interval.
	again := r.signalsLocked("s", map[string]any{"_speech_started_at": start}, now.Add(3*time.Second), end)["face"].(map[string]any)
	if again["dominant_label"] != "happy" {
		t.Fatal("waiting face replaced speech summary", again)
	}
}
func TestShortOrAmbiguousObservationsDoNotForceAnEmotion(t *testing.T) {
	now := time.Now()
	one := summarizeFace([]faceSample{{now, emotionFixture("sad")}})
	if one["dominant_label"] != nil || one["status"] != "insufficient_samples" {
		t.Fatal(one)
	}
	ambiguous := emotionFixture("happy")
	scores := ambiguous["expression"].(map[string]any)["scores"].(map[string]any)
	for k := range scores {
		scores[k] = .125
	}
	both := summarizeFace([]faceSample{{now.Add(-time.Second), ambiguous}, {now, ambiguous}})
	if both["dominant_label"] != nil || both["status"] != "mixed_or_uncertain" {
		t.Fatal(both)
	}
	empty := summarizeFace([]faceSample{{now, map[string]any{"face_detected": false}}})
	if empty["dominant_label"] != nil || empty["valid_face_count"] != 0 {
		t.Fatal(empty)
	}
}
