package provider

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Qwen-Audio-TTS Flash allows 180 requests/minute and can enforce a shorter
// window. All synthesis lanes and standalone calls on this Bus share one gate.
// The first task starts immediately; cancelled waiters reserve no future slot.
const ttsRequestInterval = 400 * time.Millisecond

type ttsRequestPacer struct {
	mu   sync.Mutex
	next time.Time
}

func (p *ttsRequestPacer) wait(ctx context.Context) error {
	for {
		p.mu.Lock()
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return err
		}
		delay := time.Until(p.next)
		if delay <= 0 {
			p.next = time.Now().Add(ttsRequestInterval)
			p.mu.Unlock()
			return nil
		}
		p.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type audioTTSError struct{ code, message string }

func (e *audioTTSError) Error() string {
	return "百炼情感 TTS 错误 " + e.code + "：" + e.message
}

func IsTTSRateLimit(err error) bool {
	var remote *audioTTSError
	return errors.As(err, &remote) && (remote.code == "Throttling.RateQuota" || remote.code == "Throttling.BurstRate")
}

func TTSFailureCode(err error) string {
	if IsTTSRateLimit(err) {
		return "rate_limited"
	}
	return "tts_failed"
}
