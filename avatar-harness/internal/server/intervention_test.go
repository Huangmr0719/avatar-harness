package server

import (
	"avatarharness/internal/config"
	"avatarharness/internal/intervention"
	"encoding/json"
	"testing"
)

func TestSupportAPIIsolationRevisionFeedbackAndClear(t *testing.T) {
	c := config.Default()
	a := setup(t, &c)
	sid := a.Runtime.Store.Create(c.Characters[0].ID).ID
	other := a.Runtime.Store.Create(c.Characters[0].ID).ID
	path := "/api/sessions/" + sid + "/intervention"
	res := request(t, a, "POST", path, intervention.Command{Operation: "start", Mode: intervention.Mindfulness})
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	var s intervention.State
	json.Unmarshal(res.Body.Bytes(), &s)
	if s.Version != 1 || s.Execution.Kind != "video" || a.Runtime.Store.Intervention(other).Active() {
		t.Fatal(s)
	}
	for _, cmd := range []intervention.Command{
		{Operation: "stop", Version: 0},
		{Operation: "feedback", Version: s.Version, ExecutionID: s.Execution.ID, Feedback: "helpful"},
		{Operation: "playback", Version: s.Version, ExecutionID: "wrong", Event: "playing"},
	} {
		if request(t, a, "POST", path, cmd).Code != 409 {
			t.Fatal("invalid support mutation accepted", cmd)
		}
	}
	if request(t, a, "GET", path, nil).Code != 200 {
		t.Fatal("read")
	}
	if request(t, a, "POST", path, intervention.Command{Operation: "stop", Version: s.Version}).Code != 200 {
		t.Fatal("stop")
	}
	request(t, a, "DELETE", "/api/sessions/"+sid+"/messages", nil)
	if a.Runtime.Store.Intervention(sid).Active() {
		t.Fatal("support remained after clear")
	}
	if request(t, a, "GET", "/api/sessions/missing/intervention", nil).Code != 404 {
		t.Fatal("session access")
	}
}
