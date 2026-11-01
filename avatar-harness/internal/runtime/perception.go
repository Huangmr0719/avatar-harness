package runtime

import (
	"math"
	"sort"
	"time"

	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

type faceSample struct {
	at   time.Time
	face map[string]any
}
type perceptionSession struct {
	token   string
	frame   int64
	samples []faceSample
}

func (r *Runtime) BeginPerception(sid string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	token := store.ID()
	r.perception[sid] = &perceptionSession{token: token}
	return token
}
func (r *Runtime) EndPerception(sid, token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p := r.perception[sid]; p != nil && p.token == token {
		delete(r.perception, sid)
	}
}
func (r *Runtime) PerceptionActive(sid, token string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.perception[sid]
	return p != nil && p.token == token
}
func (r *Runtime) ObserveFace(sid, token string, frame int64, at time.Time, face map[string]any) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.perception[sid]
	if p == nil || p.token != token || frame <= p.frame || time.Since(at) > 4*time.Second {
		return false
	}
	p.frame = frame
	p.samples = append(p.samples, faceSample{at, face})
	cutoff := at.Add(-2 * time.Minute)
	first := 0
	for first < len(p.samples) && p.samples[first].at.Before(cutoff) {
		first++
	}
	if len(p.samples)-first > 128 {
		first = len(p.samples) - 128
	}
	p.samples = append([]faceSample(nil), p.samples[first:]...)
	return true
}

func (r *Runtime) signalsLocked(sid string, audio map[string]any, now, end time.Time) map[string]any {
	signals := map[string]any{}
	if p := r.perception[sid]; p != nil {
		start, voiceWindow := audio["_speech_started_at"].(time.Time)
		kind := "recent_before_submit"
		if voiceWindow && !start.IsZero() && !end.Before(start) {
			kind = "utterance"
		} else {
			end = now
			start = end.Add(-2 * time.Second)
		}
		// A delayed ASR result keeps the original utterance boundaries; faces while
		// waiting for the assistant cannot change what was observed during speech.
		if now.Sub(end) <= 20*time.Second {
			overall := []faceSample{}
			ending := []faceSample{}
			for _, sample := range p.samples {
				if !sample.at.Before(start) && !sample.at.After(end) {
					overall = append(overall, sample)
					if !sample.at.Before(end.Add(-2 * time.Second)) {
						ending = append(ending, sample)
					}
				}
			}
			summary := summarizeFace(overall)
			summary["ending"] = summarizeFace(ending)
			summary["window"] = map[string]any{"kind": kind, "duration_ms": end.Sub(start).Milliseconds(), "age_ms": now.Sub(end).Milliseconds()}
			signals["face"] = summary
		}
	}
	if emotion := provider.String(audio["emotion"]); emotion != "" && len(emotion) <= 64 {
		signals["voice"] = map[string]any{"source": r.Config.Providers["asr"].Model, "emotion": emotion, "language": provider.String(audio["language"]), "confidence": nil}
	}
	return signals
}

func summarizeFace(samples []faceSample) map[string]any {
	result := map[string]any{"status": "insufficient_samples", "sample_count": len(samples), "valid_face_count": 0, "dominant_label": nil, "score_type": "mean_model_scores_uncalibrated"}
	scoreSum := map[string]float64{}
	scoreCount := 0
	auSum := map[string]float64{}
	auCount := map[string]int{}
	votes := map[string]int{}
	valid := 0
	var first, last time.Time
	sources := map[string]bool{}
	for _, sample := range samples {
		face := sample.face
		if face["face_detected"] != true {
			continue
		}
		valid++
		if first.IsZero() {
			first = sample.at
		}
		last = sample.at
		if source := provider.String(face["source"]); source != "" {
			sources[source] = true
		}
		aus, _ := face["au_intensities"].(map[string]any)
		for k, v := range aus {
			n := provider.Number(v)
			if !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= 5 {
				auSum[k] += n
				auCount[k]++
			}
		}
		expression, _ := face["expression"].(map[string]any)
		scores, _ := expression["scores"].(map[string]any)
		if len(scores) == 0 {
			continue
		}
		usable := map[string]float64{}
		total := 0.0
		for k, v := range scores {
			n := provider.Number(v)
			if !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= 1 {
				usable[k] = n
				total += n
			}
		}
		if len(usable) != 8 || total <= 0 {
			continue
		}
		scoreCount++
		for k, v := range usable {
			scoreSum[k] += v / total
		}
		votes[provider.String(expression["label"])]++
	}
	result["valid_face_count"] = valid
	result["expression_sample_count"] = scoreCount
	if len(samples) > 0 {
		result["face_coverage"] = float64(valid) / float64(len(samples))
	}
	if valid > 0 {
		result["observed_span_ms"] = last.Sub(first).Milliseconds()
	}
	aus := map[string]any{}
	for k, v := range auSum {
		aus[k] = math.Round(v/float64(auCount[k])*1000) / 1000
	}
	if len(aus) > 0 {
		result["au_intensities_mean"] = aus
		result["au_intensity_scale"] = []int{0, 5}
	}
	names := []string{}
	for s := range sources {
		names = append(names, s)
	}
	sort.Strings(names)
	result["sources"] = names
	if scoreCount == 0 {
		return result
	}
	labels := []string{}
	for k := range scoreSum {
		labels = append(labels, k)
	}
	sort.Strings(labels)
	distribution := map[string]any{}
	winner := ""
	top := 0.0
	entropy := 0.0
	for _, k := range labels {
		p := scoreSum[k] / float64(scoreCount)
		distribution[k] = math.Round(p*10000) / 10000
		if p > top {
			winner = k
			top = p
		}
		if p > 0 {
			entropy -= p * math.Log(p)
		}
	}
	result["distribution"] = distribution
	result["candidate_label"] = winner
	result["mean_score"] = top
	result["agreement"] = float64(votes[winner]) / float64(scoreCount)
	result["normalized_entropy"] = entropy / math.Log(float64(len(labels)))
	if scoreCount >= 2 {
		result["status"] = "mixed_or_uncertain"
		if top >= .5 {
			result["dominant_label"] = winner
			result["status"] = "observed"
		}
	}
	return result
}
