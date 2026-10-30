package store

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Session struct {
	ID          string  `json:"id"`
	CharacterID string  `json:"character_id"`
	Title       string  `json:"title"`
	CreatedAt   float64 `json:"created_at"`
}
type Event struct {
	Seq       int64          `json:"seq"`
	SessionID string         `json:"session_id"`
	TurnID    string         `json:"turn_id,omitempty"`
	Type      string         `json:"type"`
	At        float64        `json:"at"`
	Data      map[string]any `json:"data"`
}
type record struct {
	Kind      string           `json:"kind"`
	Session   *Session         `json:"session,omitempty"`
	Event     *Event           `json:"event,omitempty"`
	SessionID string           `json:"session_id,omitempty"`
	Messages  []map[string]any `json:"messages,omitempty"`
}
type Store struct {
	mu        sync.Mutex
	sessions  map[string]Session
	events    map[string][]Event
	history   map[string][]map[string]any
	subs      map[string]map[chan Event]bool
	seq       int64
	file      *os.File
	writer    *bufio.Writer
	done      chan struct{}
	flushed   chan struct{}
	lastError error
}

func ID() string { var b [12]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "sessions.jsonl")
	s := &Store{sessions: map[string]Session{}, events: map[string][]Event{}, history: map[string][]map[string]any{}, subs: map[string]map[chan Event]bool{}, done: make(chan struct{}), flushed: make(chan struct{})}
	if raw, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(raw)
		scanner.Buffer(make([]byte, 8192), 8<<20)
		for scanner.Scan() {
			var rec record
			if err = json.Unmarshal(scanner.Bytes(), &rec); err != nil {
				raw.Close()
				return nil, fmt.Errorf("历史日志损坏，原文件已保留：%w", err)
			}
			s.restore(rec)
		}
		err = scanner.Err()
		raw.Close()
		if err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	s.file = f
	s.writer = bufio.NewWriterSize(f, 64<<10)
	legacyPath := filepath.Join(dir, "sessions.sqlite3")
	marker := filepath.Join(dir, "legacy-import.done")
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		if _, err := os.Stat(legacyPath); err == nil {
			if err := s.importSQLite(legacyPath); err != nil {
				fmt.Fprintf(os.Stderr, "旧历史未导入（原文件保留，下次启动重试）：%v\n", err)
			} else {
				_ = os.WriteFile(marker, []byte("legacy SQLite imported read-only\n"), 0600)
			}
		}
	}
	go func() {
		defer close(s.flushed)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.mu.Lock()
				s.flush()
				s.mu.Unlock()
			case <-s.done:
				return
			}
		}
	}()
	return s, nil
}
func (s *Store) restore(r record) {
	switch r.Kind {
	case "session":
		if r.Session != nil {
			s.sessions[r.Session.ID] = *r.Session
		}
	case "event":
		if r.Event != nil {
			e := *r.Event
			s.events[e.SessionID] = append(s.events[e.SessionID], e)
			if e.Seq > s.seq {
				s.seq = e.Seq
			}
		}
	case "history":
		s.history[r.SessionID] = append(s.history[r.SessionID], r.Messages...)
	case "clear":
		delete(s.history, r.SessionID)
		delete(s.events, r.SessionID)
	}
}
func (s *Store) append(r record) {
	raw, err := json.Marshal(r)
	if err == nil {
		_, err = s.writer.Write(append(raw, '\n'))
	}
	if err != nil {
		s.lastError = err
	}
}
func (s *Store) flush() {
	if err := s.writer.Flush(); err != nil {
		s.lastError = err
	}
}
func (s *Store) Close() error {
	close(s.done)
	<-s.flushed
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flush()
	err := s.file.Sync()
	closeErr := s.file.Close()
	if s.lastError != nil {
		return s.lastError
	}
	if err != nil {
		return err
	}
	return closeErr
}
func (s *Store) Create(character string) Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := Session{ID: ID(), CharacterID: character, Title: "当前会话", CreatedAt: float64(time.Now().UnixMilli()) / 1000}
	s.sessions[item.ID] = item
	s.append(record{Kind: "session", Session: &item})
	return item
}
func (s *Store) Session(id string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[id]
	return v, ok
}
func (s *Store) Sessions() []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Session, 0, len(s.sessions))
	for _, v := range s.sessions {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}
func (s *Store) Emit(sid, tid, kind string, data map[string]any) Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	event := Event{s.seq, sid, tid, kind, float64(time.Now().UnixMilli()) / 1000, data}
	saved := event
	saved.Data = map[string]any{}
	for key, value := range data {
		if key != "audio_base64" && key != "frames" {
			saved.Data[key] = value
		}
	}
	s.events[sid] = append(s.events[sid], saved)
	if len(s.events[sid]) > 4000 {
		s.events[sid] = s.events[sid][len(s.events[sid])-4000:]
	}
	s.append(record{Kind: "event", Event: &saved})
	for queue := range s.subs[sid] {
		select {
		case queue <- event:
		default:
			close(queue)
			delete(s.subs[sid], queue)
		}
	}
	return event
}
func (s *Store) Events(sid string, after int64) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Event{}
	for _, e := range s.events[sid] {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return out
}
func (s *Store) Subscribe(sid string) ([]Event, chan Event, func()) {
	s.mu.Lock()
	if s.subs[sid] == nil {
		s.subs[sid] = map[chan Event]bool{}
	}
	queue := make(chan Event, 256)
	s.subs[sid][queue] = true
	history := append([]Event{}, s.events[sid]...)
	s.mu.Unlock()
	return history, queue, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.subs[sid][queue] {
			delete(s.subs[sid], queue)
			close(queue)
		}
	}
}
func (s *Store) AddHistory(sid string, messages []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history[sid] = append(s.history[sid], messages...)
	s.append(record{Kind: "history", SessionID: sid, Messages: messages})
}
func (s *Store) History(sid string, limit int) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.history[sid]
	start := len(all) - limit
	if start < 0 {
		start = 0
	}
	for start < len(all) && all[start]["role"] != "user" {
		start++
	}
	return append([]map[string]any{}, all[start:]...)
}
func (s *Store) Clear(sid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.events, sid)
	delete(s.history, sid)
	s.append(record{Kind: "clear", SessionID: sid})
}
func (s *Store) Error() error { s.mu.Lock(); defer s.mu.Unlock(); return s.lastError }
func (s *Store) importSQLite(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		return fmt.Errorf("缺少一次性导入工具 sqlite3")
	}
	if wal, err := os.Stat(path + "-wal"); err == nil && wal.Size() > 0 {
		return fmt.Errorf("请先停止旧服务并完成 SQLite WAL checkpoint；当前文件未修改")
	}
	abs, _ := filepath.Abs(path)
	uri := (&url.URL{Scheme: "file", Path: abs, RawQuery: "immutable=1"}).String()
	query := func(sql string, out any) error {
		raw, err := exec.Command(bin, "-readonly", "-json", uri, sql).Output()
		if err != nil {
			return err
		}
		if len(raw) == 0 {
			return nil
		}
		return json.Unmarshal(raw, out)
	}
	var sessions []Session
	var events []struct {
		Seq       int64   `json:"seq"`
		SessionID string  `json:"session_id"`
		TurnID    string  `json:"turn_id"`
		Type      string  `json:"type"`
		At        float64 `json:"at"`
		Data      string  `json:"data"`
	}
	var histories []struct {
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
	}
	if err = query("SELECT * FROM sessions ORDER BY created_at", &sessions); err != nil {
		return err
	}
	if err = query("SELECT * FROM events ORDER BY seq", &events); err != nil {
		return err
	}
	if err = query("SELECT session_id,message FROM history ORDER BY id", &histories); err != nil {
		return err
	}
	imported := map[string]bool{}
	for _, item := range sessions {
		if _, exists := s.sessions[item.ID]; exists {
			continue
		}
		imported[item.ID] = true
		s.sessions[item.ID] = item
		s.append(record{Kind: "session", Session: &item})
	}
	for _, old := range events {
		if !imported[old.SessionID] {
			continue
		}
		var data map[string]any
		if json.Unmarshal([]byte(old.Data), &data) != nil {
			continue
		}
		s.seq++
		event := Event{s.seq, old.SessionID, old.TurnID, old.Type, old.At, data}
		rec := record{Kind: "event", Event: &event}
		s.restore(rec)
		s.append(rec)
	}
	for _, old := range histories {
		if !imported[old.SessionID] {
			continue
		}
		var message map[string]any
		if json.Unmarshal([]byte(old.Message), &message) != nil {
			continue
		}
		rec := record{Kind: "history", SessionID: old.SessionID, Messages: []map[string]any{message}}
		s.restore(rec)
		s.append(rec)
	}
	s.flush()
	return s.file.Sync()
}
