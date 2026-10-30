package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyConfigurationAndSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Version = 1
	raw, _ := json.Marshal(c)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveSecret(filepath.Join(filepath.Dir(path), "secrets.env"), "HARNESS_TEST_SECRET", "test-only-not-a-real-key"); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("HARNESS_TEST_SECRET")
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Providers["llm"].Kind != "deferred" {
		t.Fatal("legacy config changed")
	}
	loaded.Providers["llm"] = Provider{Kind: "openai", BaseURL: "http://model.test/v1", Model: "fixture", APIKeyEnv: "HARNESS_TEST_SECRET", TimeoutMS: 1000}
	state, _ := json.Marshal(State(loaded.Providers["llm"]))
	if strings.Contains(string(state), "test-only-not-a-real-key") {
		t.Fatal("secret leaked")
	}
	if err = Save(loaded, path); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("config permissions")
	}
}
func TestPythonFactoryRequiresExternalService(t *testing.T) {
	c := Default()
	c.Providers["tts"] = Provider{Kind: "internal", Factory: "python_model:generate", TimeoutMS: 1000}
	if c.Validate() == nil {
		t.Fatal("Python must not execute in Go runtime")
	}
}

func TestBailianPresetPreservesCharactersToolsAndRenderer(t *testing.T) {
	c := Default()
	c.Characters[0].Renderer = Renderer{Kind: "scene", AssetURL: "/models/sample.vrm"}
	c.Providers["avatar"] = Provider{Kind: "fastapi", BaseURL: "http://avatar.test", TimeoutMS: 2000}
	if err := c.UseBailian("workspace123"); err != nil {
		t.Fatal(err)
	}
	if c.Providers["avatar"].Kind != "fastapi" || c.Characters[0].Renderer.AssetURL != "/models/sample.vrm" || len(c.Tools) != 2 {
		t.Fatal("preset replaced existing assets or tools")
	}
	for _, role := range []string{"llm", "asr", "tts"} {
		p := c.Providers[role]
		if p.APIKeyEnv != BailianKeyEnv || !strings.Contains(p.BaseURL, "workspace123.cn-beijing.maas.aliyuncs.com") || !p.Streaming {
			t.Fatal(role, p)
		}
	}
	if c.Providers["llm"].Parameters["enable_thinking"] != false {
		t.Fatal("interactive preset enabled slow thinking mode")
	}
	if p := c.Providers["tts"]; p.Model != BailianTTSModel || !strings.HasSuffix(p.BaseURL, "/api-ws/v1/inference") || p.Parameters["voice"] != BailianTTSVoice {
		t.Fatal("emotion-capable TTS preset uses incompatible endpoint or voice", p)
	}
	if _, err := BailianProviders("workspace/unsafe"); err == nil {
		t.Fatal("invalid workspace ID accepted")
	}
}

func TestBailianTransportIsRestrictedToSpeechRoles(t *testing.T) {
	for _, role := range []string{"llm", "avatar"} {
		c := Default()
		c.Providers[role] = Provider{Kind: "bailian", BaseURL: "wss://speech.test/realtime", APIKeyEnv: BailianKeyEnv, Model: "fixture", TimeoutMS: 1000}
		if c.Validate() == nil {
			t.Fatal("speech transport accepted for", role)
		}
	}
}

func TestTTSChunkWaitDefaultsAndBounds(t *testing.T) {
	c := Default()
	c.Limits.TTSFirstChunkMS, c.Limits.TTSChunkMS = 0, 0
	if err := c.Validate(); err != nil || c.Limits.TTSFirstChunkMS != 180 || c.Limits.TTSChunkMS != 400 {
		t.Fatal(c.Limits, err)
	}
	for _, invalid := range []Limits{
		{TTSFirstChunkMS: -1, TTSChunkMS: 400},
		{TTSFirstChunkMS: 1001, TTSChunkMS: 400},
		{TTSFirstChunkMS: 180, TTSChunkMS: 99},
		{TTSFirstChunkMS: 180, TTSChunkMS: 2001},
	} {
		c := Default()
		c.Limits.TTSFirstChunkMS, c.Limits.TTSChunkMS = invalid.TTSFirstChunkMS, invalid.TTSChunkMS
		if c.Validate() == nil {
			t.Fatal("invalid text-buffer deadline accepted", invalid)
		}
	}
}

func TestLAMLocalAssetUsesZipEndpoint(t *testing.T) {
	c := Default()
	c.Characters[0].AssetPath = "/tmp/avatar.zip"
	c.Characters[0].Renderer = Renderer{Kind: "lam"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Characters[0].Renderer.AssetURL != "/api/characters/"+c.Characters[0].ID+"/asset/avatar.zip" {
		t.Fatal(c.Characters[0].Renderer)
	}
}
