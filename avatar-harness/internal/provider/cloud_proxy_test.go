package provider

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
	"github.com/gorilla/websocket"
)

func connectProxy(t *testing.T, proxyAuth string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	count := &atomic.Int32{}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", 405)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("cloud bearer token exposed to proxy")
		}
		if r.Header.Get("Proxy-Authorization") != proxyAuth {
			t.Error("proxy authentication lost")
		}
		count.Add(1)
		target, err := net.DialTimeout("tcp", r.Host, time.Second)
		if err != nil {
			http.Error(w, "dial failed", 502)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			target.Close()
			return
		}
		defer client.Close()
		defer target.Close()
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() { _, _ = io.Copy(target, client); target.Close() }()
		_, _ = io.Copy(client, target)
	}))
	t.Cleanup(proxy.Close)
	return proxy, count
}

func TestCloudHTTPSProxyReusesConnectionAndKeepsModelsDirect(t *testing.T) {
	t.Setenv("PROXY_TEST_KEY", "unit-test-only")
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer unit-test-only" {
			t.Error("cloud authentication lost")
		}
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials exposed to upstream")
		}
		_, _ = w.Write([]byte("cloud"))
	}))
	defer remote.Close()
	proxy, count := connectProxy(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-test-only")))
	u, _ := url.Parse(proxy.URL)
	u.User = url.UserPassword("proxy-user", "proxy-test-only")
	c := config.Default()
	c.CloudProxyURL = u.String()
	b, err := New(&c, remote.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer b.cloudClient.CloseIdleConnections()
	for range 2 {
		res, err := b.post(context.Background(), config.Provider{Kind: "openai", BaseURL: remote.URL, APIKeyEnv: "PROXY_TEST_KEY"}, "/fixture", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if string(raw) != "cloud" {
			t.Fatal(string(raw))
		}
	}
	if count.Load() != 1 {
		t.Fatalf("cloud transport did not reuse CONNECT: %d", count.Load())
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("local model")) }))
	defer local.Close()
	res, err := b.post(context.Background(), config.Provider{Kind: "fastapi", BaseURL: local.URL}, "/invoke", Request{Capability: "avatar"})
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if count.Load() != 1 {
		t.Fatal("local model went through cloud proxy")
	}
}

func TestBailianWebSocketUsesCloudCONNECTProxy(t *testing.T) {
	t.Setenv("PROXY_TEST_KEY", "unit-test-only")
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer unit-test-only" {
			t.Error("Bailian bearer missing")
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"type": "proxy-fixture"})
	}))
	defer remote.Close()
	proxy, count := connectProxy(t, "")
	c := config.Default()
	c.CloudProxyURL = proxy.URL
	b, err := New(&c, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := b.dialBailian(context.Background(), config.Provider{BaseURL: "ws" + strings.TrimPrefix(remote.URL, "http"), Model: "asr-fixture", APIKeyEnv: "PROXY_TEST_KEY"}, "asr")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var message map[string]any
	if err = conn.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message["type"] != "proxy-fixture" || count.Load() != 1 {
		t.Fatal(message, count.Load())
	}
}

func TestProxyURLRejectsUnsupportedSchemesAndPreservesBailianConfig(t *testing.T) {
	c := config.Default()
	c.CloudProxyURL = "http://127.0.0.1:3128"
	if err := c.UseBailian(""); err != nil {
		t.Fatal(err)
	}
	if c.CloudProxyURL != "http://127.0.0.1:3128" {
		t.Fatal("preset discarded proxy")
	}
	for _, value := range []string{"ftp://127.0.0.1:3128", "http://", "http://127.0.0.1:3128/path"} {
		c.CloudProxyURL = value
		if _, err := New(&c, nil); err == nil {
			t.Fatalf("accepted invalid proxy %s", value)
		}
	}
}
