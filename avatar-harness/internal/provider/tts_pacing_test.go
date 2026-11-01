package provider

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTTSPacingCancellationDoesNotReserveFutureSlots(t *testing.T) {
	var p ttsRequestPacer
	if err := p.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := p.next
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.wait(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("cancel blocked in pacing queue")
	}
	if !p.next.Equal(next) {
		t.Fatal("cancelled task reserved a future request slot")
	}
}

// Concurrent synthesis lanes and standalone calls share the same account/model
// quota. A per-lane sleep would still fail this fixture.
func TestQwenAudioTTSPacesConcurrentLanesAndStandaloneCalls(t *testing.T) {
	var mu sync.Mutex
	var starts []time.Time
	b := audioTTSFixture(t, func(conn *websocket.Conn) {
		id, _ := acceptAudioTask(t, conn)
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{1, 0})
		finishAudioTask(conn, id)
	})
	var wg sync.WaitGroup
	begin := make(chan struct{})
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-begin
			emit := func(map[string]any) error { return nil }
			var err error
			if i < 2 {
				stream := b.NewTTSStream(context.Background(), nil, nil)
				defer stream.Close()
				err = stream.Stream("并发通道。", emit)
			} else {
				err = b.Stream(context.Background(), "tts", map[string]any{"text": "独立调用。"}, nil, emit)
			}
			if err != nil {
				t.Error(err)
			}
		}()
	}
	close(begin)
	wg.Wait()
	sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })
	if len(starts) != 4 {
		t.Fatalf("received %d requests", len(starts))
	}
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(starts[i-1]); gap < 350*time.Millisecond {
			t.Errorf("TTS requests burst across lanes: gap=%s; want at least 350ms", gap)
		}
	}
}
