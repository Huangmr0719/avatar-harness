package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"avatarharness/internal/config"
	"avatarharness/internal/store"
	"github.com/gorilla/websocket"
)

func avatarZIP(t *testing.T, names ...string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte("fixture:" + name))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func importZIP(a *App, data []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/characters/import?name=New+avatar", bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/zip")
	w := httptest.NewRecorder()
	a.Handler.ServeHTTP(w, r)
	return w
}

func TestImportedAvatarIsSelectableServedListedAndPersists(t *testing.T) {
	c := config.Default()
	c.AvatarCreator = &config.AvatarCreator{BaseURL: "http://127.0.0.1:18767"}
	c.Characters[0].Voice = "configured-voice"
	a := setup(t, &c)
	data := avatarZIP(t, "person/skin.glb", "person/offset.ply", "person/animation.glb")
	res := importZIP(a, data)
	if res.Code != 201 {
		t.Fatal(res.Code, res.Body.String())
	}
	var ch config.Character
	if err := json.Unmarshal(res.Body.Bytes(), &ch); err != nil {
		t.Fatal(err)
	}
	if ch.Voice != "configured-voice" || ch.Renderer.Driver != "remote" || ch.Renderer.Kind != "lam" || len(c.Characters) != 1 {
		t.Fatal(ch)
	}
	if res := importZIP(a, data); res.Code != 200 || len(a.Runtime.Characters()) != 2 {
		t.Fatal("duplicate import", res.Code, res.Body.String())
	}
	asset := request(t, a, "GET", ch.Renderer.AssetURL, nil)
	if asset.Code != 200 || !bytes.Equal(asset.Body.Bytes(), data) {
		t.Fatal("asset changed", asset.Code)
	}
	session := request(t, a, "POST", "/api/sessions", map[string]any{"character_id": ch.ID})
	if session.Code != 201 {
		t.Fatal(session.Code, session.Body.String())
	}
	result := a.Runtime.Bus.Invoke(context.Background(), "list_characters", map[string]any{}, nil)
	encoded, _ := json.Marshal(result.Output)
	if result.Status != "ok" || !strings.Contains(string(encoded), ch.ID) {
		t.Fatal(result)
	}
	// Reopen the same state directory, without changing configuration or defaults.
	reopened, err := store.Open(strings.TrimSuffix(a.Runtime.Store.CharacterAssetDir(), "/characters"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if items := reopened.Characters(); len(items) != 1 || items[0].ID != ch.ID || items[0].AssetPath != ch.AssetPath {
		t.Fatal("import lost on restart", items)
	}
}

func TestAvatarImportRejectsIncompleteAndUnsafeZIP(t *testing.T) {
	c := config.Default()
	c.AvatarCreator = &config.AvatarCreator{BaseURL: "http://127.0.0.1:18767"}
	a := setup(t, &c)
	for _, data := range [][]byte{[]byte("not zip"), avatarZIP(t, "skin.glb"), avatarZIP(t, "../skin.glb", "offset.ply", "animation.glb")} {
		if res := importZIP(a, data); res.Code != 422 {
			t.Fatal(res.Code, res.Body.String())
		}
	}
	if len(a.Runtime.Characters()) != 1 {
		t.Fatal("failed import changed character list")
	}
	r := httptest.NewRequest("POST", "/api/characters/import", strings.NewReader("text"))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	a.Handler.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal(w.Code)
	}
}

func TestCreatorProxyKeepsSingleOriginAndWebSocket(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("gateway credential forwarded to creator")
		}
		if r.Header.Get("X-Forwarded-Host") == "" {
			t.Error("public origin missing")
		}
		if r.URL.Path == "/queue/join" {
			ws, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer ws.Close()
			_ = ws.WriteJSON(map[string]any{"msg": "send_hash"})
			return
		}
		io.WriteString(w, r.URL.RequestURI())
	}))
	defer upstream.Close()
	c := config.Default()
	c.AvatarCreator = &config.AvatarCreator{BaseURL: upstream.URL}
	a := setup(t, &c)
	r := httptest.NewRequest("GET", "/create-avatar/assets/main.js?version=1", nil)
	r.SetBasicAuth("gateway", "private-test")
	w := httptest.NewRecorder()
	a.Handler.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "/assets/main.js?version=1" {
		t.Fatal(w.Code, w.Body.String())
	}
	server := httptest.NewServer(a.Handler)
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/create-avatar/queue/join", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var msg map[string]any
	if err := ws.ReadJSON(&msg); err != nil || msg["msg"] != "send_hash" {
		t.Fatal(msg, err)
	}
}
