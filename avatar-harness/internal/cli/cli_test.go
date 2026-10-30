package cli

import (
	"avatarharness/internal/config"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWizardPersistsDeferredConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var out bytes.Buffer
	input := strings.NewReader("5\n3\n3\n3\n终端角色\n8771\n")
	if err := Execute([]string{"--config", path, "init"}, input, &out); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil || c.Port != 8771 || c.Characters[0].Name != "终端角色" {
		t.Fatal(c, err)
	}
	if !strings.Contains(out.String(), "先配置服务，再展示 Web") {
		t.Fatal(out.String())
	}
}
func TestRegisterFastAPIProviderFromTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = config.Save(config.Default(), path)
	var out bytes.Buffer
	input := strings.NewReader("1\nhttp://127.0.0.1:9901\ny\n\n2000\n")
	if err := Execute([]string{"--config", path, "configure", "tts"}, input, &out); err != nil {
		t.Fatal(err)
	}
	c, _ := config.Load(path)
	p := c.Providers["tts"]
	if p.Kind != "fastapi" || p.BaseURL != "http://127.0.0.1:9901" || !p.Streaming {
		t.Fatal(p)
	}
}

func TestConfigureBailianDefaultsAndOneSharedSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := config.Default()
	c.Characters[0].Renderer = config.Renderer{Kind: "scene", AssetURL: "/models/sample.vrm"}
	_ = config.Save(c, path)
	t.Setenv(config.BailianKeyEnv, "")
	var out bytes.Buffer
	if err := Execute([]string{"--config", path, "configure", "bailian", "--defaults"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil || c.Providers["asr"].Kind != "bailian" || c.Characters[0].Renderer.AssetURL != "/models/sample.vrm" {
		t.Fatal(c, err)
	}
	if config.State(c.Providers["tts"])["status"] != "missing_key" {
		t.Fatal("preset falsely reported ready")
	}
	input := strings.NewReader("workspace123\n\n\n\nEthan\nfixture-one-shared-secret\n")
	if err := Execute([]string{"--config", path, "configure", "bailian"}, input, &out); err != nil {
		t.Fatal(err)
	}
	c, err = config.Load(path)
	if err != nil || !strings.Contains(c.Providers["asr"].BaseURL, "workspace123.cn-beijing") || c.Providers["tts"].Parameters["voice"] != "Ethan" {
		t.Fatal(c, err)
	}
	if os.Getenv(config.BailianKeyEnv) != "fixture-one-shared-secret" || strings.Contains(out.String(), "fixture-one-shared-secret") {
		t.Fatal("secret missing or printed to terminal output")
	}
}

func TestStartupDetectsCredentialsSavedAfterServerStarted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	_ = config.Save(config.Default(), path)
	_ = os.WriteFile(filepath.Join(dir, "server.json"), []byte(`{}`), 0600)
	before := time.Now().Add(-time.Minute)
	_ = os.Chtimes(path, before, before)
	if configurationChanged(path) {
		t.Fatal("unchanged configuration triggered restart")
	}
	_ = os.WriteFile(filepath.Join(dir, "secrets.env"), []byte("TEST_ONLY=fixture\n"), 0600)
	after := time.Now().Add(time.Second)
	_ = os.Chtimes(filepath.Join(dir, "secrets.env"), after, after)
	if !configurationChanged(path) {
		t.Fatal("startup reused a process with stale credentials")
	}
}
