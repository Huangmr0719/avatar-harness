// harness-bench measures the control plane with an explicitly scripted HTTP
// provider. It does not benchmark a language model, audio, GPU, or AV feedback.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"avatarharness/internal/server"
	"avatarharness/internal/store"
)

func delta(w http.ResponseWriter, value map[string]any) {
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": value}}})
	fmt.Fprintf(w, "data: %s\n\n", raw)
}
func main() {
	count := flag.Int("requests", 200, "")
	concurrency := flag.Int("concurrency", 1, "")
	output := flag.String("output", "", "")
	mode := flag.String("mode", "tool_loop", "")
	flag.Parse()
	if *count < 1 || *concurrency < 1 {
		panic("requests and concurrency must be positive")
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		if _, ok := body["tools"]; ok {
			delta(w, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "benchmark-clock", "function": map[string]any{"name": "get_current_time", "arguments": `{"timezone":"Asia/Shanghai"}`}}}})
		} else {
			messages := body["messages"].([]any)
			last := messages[len(messages)-1].(map[string]any)
			var result provider.Result
			_ = json.Unmarshal([]byte(last["content"].(string)), &result)
			delta(w, map[string]any{"content": "协议夹具读取到的时间：" + provider.String(result.Output["datetime"])})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer fixture.Close()
	c := config.Default()
	c.Providers["llm"] = config.Provider{Kind: "openai", BaseURL: fixture.URL, Model: "scripted-protocol-fixture", TimeoutMS: 2000}
	dir, err := os.MkdirTemp("", "harness-go-bench-")
	check(err)
	defer os.RemoveAll(dir)
	s, err := store.Open(dir)
	check(err)
	bus, err := provider.New(&c, nil)
	check(err)
	app := server.New(&c, s, bus)
	defer func() { app.Close(); _ = s.Close() }()
	host := httptest.NewServer(app.Handler)
	defer host.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	post := func(path string, body any, out any) error {
		raw, _ := json.Marshal(body)
		response, err := client.Post(host.URL+path, "application/json", bytes.NewReader(raw))
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode >= 300 {
			return fmt.Errorf("HTTP %d", response.StatusCode)
		}
		if out != nil {
			return json.NewDecoder(response.Body).Decode(out)
		}
		_, err = io.Copy(io.Discard, response.Body)
		return err
	}
	latencies := make([]float64, *count)
	failures := make([]string, *count)
	jobs := make(chan int)
	var wg sync.WaitGroup
	began := time.Now()
	for worker := 0; worker < *concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				func() {
					var session store.Session
					if err := post("/api/sessions", map[string]any{}, &session); err != nil {
						failures[index] = err.Error()
						return
					}
					events, err := client.Get(host.URL + "/api/sessions/" + session.ID + "/events")
					if err != nil {
						failures[index] = err.Error()
						return
					}
					defer events.Body.Close()
					started := time.Now()
					text := "现在几点？"
					if *mode == "local_tool" {
						text = `/tool get_current_time {"timezone":"Asia/Shanghai"}`
					}
					if err = post("/api/sessions/"+session.ID+"/turns", map[string]any{"text": text}, nil); err != nil {
						failures[index] = err.Error()
						return
					}
					scanner := bufio.NewScanner(events.Body)
					scanner.Buffer(make([]byte, 4096), 1<<20)
					for scanner.Scan() {
						line := scanner.Text()
						if !strings.HasPrefix(line, "data: ") {
							continue
						}
						var event store.Event
						if json.Unmarshal([]byte(line[6:]), &event) != nil {
							continue
						}
						if event.Type == "turn.error" {
							failures[index] = provider.String(event.Data["message"])
							return
						}
						if event.Type == "assistant.message" {
							latencies[index] = float64(time.Since(started).Microseconds()) / 1000
							return
						}
					}
					failures[index] = "stream ended before answer"
				}()
			}
		}()
	}
	for index := 0; index < *count; index++ {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	elapsed := time.Since(began)
	success := []float64{}
	errorsFound := []string{}
	sum := 0.0
	for index, value := range latencies {
		if failures[index] != "" {
			errorsFound = append(errorsFound, failures[index])
		} else {
			success = append(success, value)
			sum += value
		}
	}
	sort.Float64s(success)
	percentile := func(p float64) float64 {
		if len(success) == 0 {
			return 0
		}
		return success[int(math.Ceil(p*float64(len(success)))-1)]
	}
	report := map[string]any{"date": time.Now().UTC().Format(time.RFC3339), "runtime": "go", "go_version": runtime.Version(), "platform": runtime.GOOS + "/" + runtime.GOARCH, "logical_cpus": runtime.NumCPU(), "requests": *count, "concurrency": *concurrency, "mode": *mode, "success": len(success), "errors": errorsFound, "latency_ms": map[string]any{"p50": percentile(.50), "p95": percentile(.95), "p99": percentile(.99), "max": percentile(1), "mean": sum / float64(max(1, len(success)))}, "elapsed_s": elapsed.Seconds(), "requests_per_second": float64(*count) / elapsed.Seconds(), "includes": "HTTP input, SSE output, Go scheduling, JSON/schema validation, actual local time tool, buffered journal; tool_loop includes two localhost model-protocol HTTP requests", "excludes": "real model inference, ASR, TTS, avatar inference, browser rendering, network-to-cloud latency, and 3-second audiovisual SLO"}
	raw, err := json.MarshalIndent(report, "", "  ")
	check(err)
	fmt.Println(string(raw))
	if *output != "" {
		check(os.MkdirAll(filepath.Dir(*output), 0755))
		check(os.WriteFile(*output, append(raw, '\n'), 0644))
	}
	if len(errorsFound) > 0 {
		os.Exit(1)
	}
}
func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
