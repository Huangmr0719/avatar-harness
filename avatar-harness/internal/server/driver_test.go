package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
)

func TestRemoteLAMDriverProxyAndAssetDescription(t *testing.T) {
	c := config.Default()
	c.Characters[0].Renderer = config.Renderer{Kind: "lam", Driver: "remote", AssetURL: "/models/custom-avatar.zip"}
	c.Providers["avatar"] = config.Provider{Kind: "internal", Factory: "remote-driver", TimeoutMS: 1000}
	a := setup(t, &c)
	operations := []string{}
	a.Runtime.Bus.Register("remote-driver", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		operation := provider.String(req.Input["operation"])
		operations = append(operations, operation)
		if operation == "infer_window" {
			if req.Input["format"] != "pcm_f32le" || req.Input["sample_rate"] != 16000 {
				t.Error("audio protocol mismatch", req.Input["format"], req.Input["sample_rate"])
			}
			if req.Context["session_id"] == "" {
				t.Error("missing session scope")
			}
			return provider.OK(map[string]any{"frames": 64, "weights": []float64{.1}}), nil
		}
		return provider.OK(map[string]any{"backend": "cuda", "input_samples": 34133}), nil
	})
	sid := a.Runtime.Store.Create(c.Characters[0].ID).ID
	path := "/api/sessions/" + sid
	res := request(t, a, "POST", path+"/avatar", map[string]any{})
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "custom-avatar.zip") {
		t.Fatal("remote health check changed the configured character asset", res.Body.String())
	}
	res = request(t, a, "GET", path+"/driver", nil)
	if res.Code != http.StatusOK {
		t.Fatal(res.Body.String())
	}
	res = request(t, a, "POST", path+"/driver", map[string]any{"audio_base64": base64.StdEncoding.EncodeToString(make([]byte, 34133*4))})
	if res.Code != http.StatusOK {
		t.Fatal(res.Body.String())
	}
	before := len(operations)
	res = request(t, a, "POST", path+"/driver", map[string]any{"audio_base64": "bad"})
	if res.Code != http.StatusUnprocessableEntity || len(operations) != before {
		t.Fatal("invalid audio reached the GPU provider")
	}
	if operations[0] != "health" || operations[len(operations)-1] != "infer_window" {
		t.Fatal(operations)
	}
}
