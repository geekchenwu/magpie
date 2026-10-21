package provider

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
)

func groupUltraHome(t *testing.T) {
	t.Helper()
	signIn(t)
	ForgetAccounts()
	t.Cleanup(ForgetAccounts)
	models := []catalog.Model{{ID: "gpt-6.1-sol", Tiers: []string{"priority", "ultrafast"}}}
	for _, id := range []string{"codex", accountModels("codex", "me@example.com")} {
		if err := catalog.SaveLive(id, CodexBase, models); err != nil {
			t.Fatal(err)
		}
	}
	Changed()
	p, err := Find("codex")
	if err != nil || !CanUltra(*p, "gpt-6.1-sol") {
		t.Fatalf("fixture did not offer Ultrafast: provider %+v, models %v, error %v", p, UltraModels(), err)
	}
}

func TestSaveGroupUltra(t *testing.T) {
	groupUltraHome(t)
	const id = "codex/gpt-6.1-sol:high"
	if err := SaveGroup(Group{ID: "u", Members: []string{id}, Fast: []string{id},
		Ultra: []string{" " + id + " ", id, "codex/gone"}}); err != nil {
		t.Fatal(err)
	}
	g, ms, _ := FindGroup("group/u")
	if !slices.Equal(g.Ultra, []string{id}) || len(ms) != 1 || !ms[0].Ultra {
		t.Fatalf("saved ultra %v, members %+v", g.Ultra, ms)
	}
	g.RenameMember(id, "codex/gpt-6.1-sol:low")
	if err := SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	g, _, _ = FindGroup("group/u")
	if !g.IsUltra("codex/gpt-6.1-sol:low") || g.IsUltra(id) {
		t.Fatalf("effort change lost Ultrafast: %v", g.Ultra)
	}
	copy, err := CopyGroup("u", "Copy")
	if err != nil || !slices.Equal(copy.Ultra, g.Ultra) {
		t.Fatalf("copy ultra %v: %v", copy.Ultra, err)
	}
	text, err := ShareGroup("u")
	if err != nil {
		t.Fatal(err)
	}
	imported, err := ImportGroup(text)
	if err != nil || !sharedAt(t, imported.ID).IsUltra("codex/gpt-6.1-sol:low") {
		t.Fatalf("import %+v: %v", imported, err)
	}
	g.SetMemberFast("codex/gpt-6.1-sol:low", false)
	if len(g.Ultra) != 0 || len(g.Fast) != 0 {
		t.Fatalf("standard speed kept ultra %v fast %v", g.Ultra, g.Fast)
	}
	if err := SaveGroup(Group{ID: "subset", Members: []string{id}, Ultra: []string{id}}); err != nil {
		t.Fatal(err)
	}
	if g, _, _ := FindGroup("group/subset"); len(g.Ultra) != 0 {
		t.Fatalf("Ultrafast survived without Fast: %v", g.Ultra)
	}
	if err := Save(Provider{ID: "oa", Key: "k", Responses: "https://api.openai.com/v1", Models: []string{"gpt-6.1-sol"}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveGroup(Group{ID: "unsupported", Members: []string{"oa/gpt-6.1-sol"}, Fast: []string{"oa/gpt-6.1-sol"}, Ultra: []string{"oa/gpt-6.1-sol"}}); err == nil || !strings.Contains(err.Error(), "no ultrafast mode") {
		t.Fatalf("unsupported Ultra: %v", err)
	}
}

func TestGroupUltraKeepsEachNestedOccurrence(t *testing.T) {
	groupUltraHome(t)
	const id = "codex/gpt-6.1-sol"
	for _, g := range []Group{
		{ID: "first", Members: []string{id}, Fast: []string{id}},
		{ID: "second", Members: []string{id}, Fast: []string{id}, Ultra: []string{id}},
		{ID: "parent", Members: []string{"group/first", "group/second"}},
	} {
		if err := SaveGroup(g); err != nil {
			t.Fatal(err)
		}
	}
	_, ms, _ := FindGroup("group/parent")
	if len(ms) != 1 || ms[0].Ultra || len(ms[0].Also) != 1 || !ms[0].Also[0].Ultra {
		t.Fatalf("occurrences %+v", ms)
	}
	second, ok := ms[0].In("group/second")
	if !ok || !second.Ultra || !second.Below(1).Ultra {
		t.Fatalf("second occurrence %+v", second)
	}
}

func TestImportGroupUltraDowngradesOnAnotherProvider(t *testing.T) {
	fastHome(t)
	text, err := json.Marshal(SharedGroup{Magpie: ShareMark, Version: ShareVersion, Group: Group{
		ID: "u", Members: []string{"gone/gpt-6.1-sol"}, Fast: []string{"gone/gpt-6.1-sol"}, Ultra: []string{"gone/gpt-6.1-sol"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := ImportGroup(string(text))
	if err != nil {
		t.Fatal(err)
	}
	g := sharedAt(t, res.ID)
	if len(g.Fast) != 1 || len(g.Ultra) != 0 {
		t.Fatalf("imported speed: fast %v ultra %v", g.Fast, g.Ultra)
	}
}

func TestSharedPatternMemberKeepsUltra(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			groupUltraHome(t)
			const id = "codex/gpt-6.1-sol"
			g := Group{ID: "patterns", Match: []string{"codex/*"}, Fast: []string{id}, Ultra: []string{id}}
			if mixed {
				if err := Save(Provider{ID: "oa", Key: "k", Responses: "https://api.openai.com/v1", Models: []string{"gpt-6.1-sol"}}); err != nil {
					t.Fatal(err)
				}
				g.Members = []string{"oa/gpt-6.1-sol"}
			}
			if err := SaveGroup(g); err != nil {
				t.Fatal(err)
			}
			text, err := ShareGroup(g.ID)
			if err != nil {
				t.Fatal(err)
			}
			res, err := ImportGroup(text)
			if err != nil {
				t.Fatal(err)
			}
			got := sharedAt(t, res.ID)
			if !got.IsUltra(id) || !slices.Contains(got.Matched, id) {
				t.Fatalf("matched member lost Ultrafast: fast %v ultra %v matched %v", got.Fast, got.Ultra, got.Matched)
			}
			for _, stored := range load().Groups {
				if stored.ID == res.ID && slices.Contains(stored.Members, id) {
					t.Fatal("the import made a pattern member explicit")
				}
			}
		})
	}
}
