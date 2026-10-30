// Small real-service diagnostic. It prints timings, never credentials or history.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"sync"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	kernel "avatarharness/internal/runtime"
)

type sample struct {
	Model       string  `json:"model"`
	Iteration   int     `json:"iteration"`
	FirstTextMS float64 `json:"first_text_ms"`
	TotalMS     float64 `json:"total_ms"`
	Error       string  `json:"error,omitempty"`
}

func main() {
	models := flag.String("models", config.BailianLLMModel, "comma-separated Bailian model IDs")
	iterations := flag.Int("samples", 2, "number of real requests per model")
	flag.Parse()
	base, err := config.Load(config.DefaultPath())
	if err != nil {
		panic(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := []sample{}
	for _, model := range strings.Split(*models, ",") {
		wg.Add(1)
		go func(model string) {
			defer wg.Done()
			c := base
			c.Providers = map[string]config.Provider{}
			for name, p := range base.Providers {
				c.Providers[name] = p
			}
			p := c.Providers["llm"]
			p.Model = model
			p.Parameters = map[string]any{"enable_thinking": false, "max_tokens": 96}
			c.Providers["llm"] = p
			bus, err := provider.New(&c, nil)
			if err != nil {
				panic(err)
			}
			for iteration := 1; iteration <= *iterations; iteration++ {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				started := time.Now()
				r := sample{Model: model, Iteration: iteration}
				var gate kernel.AnswerGate
				err := bus.LLM(ctx, map[string]any{"messages": []map[string]any{{"role": "system", "content": "使用简洁自然的中文回答。需要工具时直接调用工具；可以回答时以 <speak/> 开头，随后直接给出一句可播报的简短答案。"}, {"role": "user", "content": "请用一句话解释什么是光合作用。"}}, "tools": bus.Spec()}, nil, func(event map[string]any) error {
					if event["type"] == "text" && gate.Feed(provider.String(event["text"]), false) != "" && r.FirstTextMS == 0 {
						r.FirstTextMS = float64(time.Since(started).Microseconds()) / 1000
					}
					return nil
				})
				cancel()
				r.TotalMS = float64(time.Since(started).Microseconds()) / 1000
				if err != nil {
					r.Error = err.Error()
				}
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}
		}(model)
	}
	wg.Wait()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(results)
}
