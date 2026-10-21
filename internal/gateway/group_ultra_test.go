package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// Read the same JSON the Routing page saves. Old versions accept it but
// discard ultra, so the regression fails on the tier actually sent.
func saveUltraGroupJSON(t *testing.T, raw string) {
	t.Helper()
	var g provider.Group
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
}

func groupUltraBackend(t *testing.T, proOut bool) (*Server, func() []string) {
	t.Helper()
	fresh(t)
	tried := tierAccounts(t, &proOut, nil)
	if !provider.UltraModels()["gpt-6-luna"] {
		t.Fatal("the Pro account did not offer Ultrafast")
	}
	to, _ := url.Parse(provider.CodexBase)
	provider.CodexBase = "https://chatgpt.com/backend-api/codex"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "chatgpt.com":
			r.URL.Scheme, r.URL.Host = to.Scheme, to.Host
		case to.Host:
		default:
			return nil, fmt.Errorf("unexpected upstream %s", r.URL.Host)
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	oldClient := http.DefaultClient
	http.DefaultClient = client
	t.Cleanup(func() { http.DefaultClient = oldClient })
	s := New()
	s.client = client
	return s, tried
}

func TestGroupUltraMemberSentUltrafast(t *testing.T) {
	for _, a := range fastAsks {
		for _, tier := range []string{"", "priority"} {
			t.Run(a.name+"/"+tier, func(t *testing.T) {
				s, tried := groupUltraBackend(t, false)
				saveUltraGroupJSON(t, `{"id":"f","members":["codex/gpt-6-luna"],"fast":["codex/gpt-6-luna"],"ultra":["codex/gpt-6-luna"]}`)
				body := a.body
				if tier != "" {
					body = strings.TrimSuffix(body, "}") + `,"service_tier":"` + tier + `"}`
				}
				r := postProto(t, s, a.path, body)
				if got := strings.Join(tried(), ","); got != "acct-pro:ultrafast" {
					t.Errorf("group member Ultrafast sent %s, want acct-pro:ultrafast", got)
				}
				if len(r.Tries) != 1 || !r.Tries[0].Fast {
					t.Errorf("not traced fast: %+v", r.Tries)
				}
			})
		}
	}
}

func TestGroupUltraMemberFallsBackToPriority(t *testing.T) {
	for _, a := range fastAsks {
		t.Run(a.name, func(t *testing.T) {
			s, tried := groupUltraBackend(t, true)
			saveUltraGroupJSON(t, `{"id":"f","members":["codex/gpt-6-luna"],"fast":["codex/gpt-6-luna"],"ultra":["codex/gpt-6-luna"]}`)
			postProto(t, s, a.path, a.body)
			if got := strings.Join(tried(), ","); got != "acct-pro:ultrafast,acct-plus:priority" {
				t.Errorf("group member fallback sent %s, want Pro Ultrafast then Plus priority", got)
			}
		})
	}
}

func TestGroupUltraFollowsRuleAndNestedGroup(t *testing.T) {
	for _, c := range []struct {
		name, parent, want string
	}{
		{"nested", `{"id":"f","members":["group/second"]}`, "acct-pro:ultrafast"},
		{"first-place", `{"id":"f","members":["group/first","group/second"]}`, "acct-plus:priority"},
		{"rule-place", `{"id":"f","members":["group/first","group/second"],"patience":-1,"rules":[{"use":"group/second","tokens":1}]}`, "acct-pro:ultrafast"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, tried := groupUltraBackend(t, false)
			saveUltraGroupJSON(t, `{"id":"first","members":["codex/gpt-6-luna"],"fast":["codex/gpt-6-luna"]}`)
			saveUltraGroupJSON(t, `{"id":"second","members":["codex/gpt-6-luna"],"fast":["codex/gpt-6-luna"],"ultra":["codex/gpt-6-luna"]}`)
			saveUltraGroupJSON(t, c.parent)
			r := postProto(t, s, "/v1/responses", `{"model":"group/f","input":"Please route this request through the second group."}`)
			if c.name == "rule-place" && (r.Rule == nil || r.Rule.N != 1 || r.Rule.Use != "group/second" || r.Rule.Unready || r.Rule.Instead != "") {
				t.Fatalf("rule did not select second: %+v", r.Rule)
			}
			if got := strings.Join(tried(), ","); got != c.want {
				t.Errorf("group membership sent %s, want %s", got, c.want)
			}
		})
	}
}
