package gui

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
)

// The Routing view saves the members sent fast and is told which models
// have a fast mode, and which members are sent in it.
func TestGroupSaveFast(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, p := range []provider.Provider{
		{ID: "oa", Name: "OpenAI", Key: "k", Responses: "https://api.openai.com/v1", Models: []string{"gpt-6.1-sol"}},
		{ID: "rl", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"glm-5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	groupRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/groups/save", strings.NewReader(
		`{"id":"sol","name":"Sol","members":["oa/gpt-6.1-sol:high","rl/glm-5"],"fast":["oa/gpt-6.1-sol:high"]}`)))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if g, _, _ := provider.FindGroup("group/sol"); !slices.Equal(g.Fast, []string{"oa/gpt-6.1-sol:high"}) {
		t.Fatalf("fast %v", g.Fast)
	}
	st := groupsState()
	for _, m := range st.Models {
		if want := m.ID == "oa/gpt-6.1-sol"; m.CanFast != want {
			t.Errorf("%s canFast %v", m.ID, m.CanFast)
		}
	}
	for _, g := range st.Groups {
		for _, m := range g.Info {
			if want := m.ID == "oa/gpt-6.1-sol:high"; m.Fast != want || m.CanFast != want {
				t.Errorf("%s fast %v canFast %v", m.ID, m.Fast, m.CanFast)
			}
		}
	}
}

func TestGroupSaveUltra(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CODEX_HOME", "")
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token":      claims(map[string]any{"email": "ultra@example.com"}),
		"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
		"refresh_token": "r", "account_id": "acct-ultra",
	}})
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), auth, 0o600); err != nil {
		t.Fatal(err)
	}
	models := []catalog.Model{{ID: "gpt-6.1-sol", Tiers: []string{"priority", "ultrafast"}}}
	for _, id := range []string{"codex", "codex@" + provider.KeyID("ultra@example.com")} {
		if err := catalog.SaveLive(id, provider.CodexBase, models); err != nil {
			t.Fatal(err)
		}
	}
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)
	mux := http.NewServeMux()
	groupRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/groups/save", strings.NewReader(
		`{"id":"u","members":["codex/gpt-6.1-sol:high"],"fast":["codex/gpt-6.1-sol:high"],"ultra":["codex/gpt-6.1-sol:high"]}`)))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	g, _, _ := provider.FindGroup("group/u")
	if !g.IsUltra("codex/gpt-6.1-sol:high") {
		t.Fatalf("Ultrafast not saved: %+v", g)
	}
	st := groupsState()
	if i := slices.IndexFunc(st.Models, func(m modelRef) bool { return m.ID == "codex/gpt-6.1-sol" }); i < 0 || !st.Models[i].CanUltra {
		t.Fatalf("Ultrafast capability missing: %+v", st.Models)
	}
	for _, g := range st.Groups {
		if g.ID == "u" && (len(g.Info) != 1 || !g.Info[0].Ultra || !g.Info[0].CanUltra || !g.Info[0].Fast) {
			t.Fatalf("member speed missing: %+v", g.Info)
		}
	}
}
