package main

import (
	"sync"
	"testing"

	"github.com/slack-go/slack"
)

func TestUsergroupHandlesPrefersHandle(t *testing.T) {
	byID := usergroupHandles([]slack.UserGroup{
		{ID: "S1", Handle: "platform-team", Name: "Platform Team"},
	})
	if byID["S1"] != "platform-team" {
		t.Errorf("handle = %q, want platform-team", byID["S1"])
	}
}

func TestUsergroupHandlesSlugifiesNameFallback(t *testing.T) {
	byID := usergroupHandles([]slack.UserGroup{
		{ID: "S1", Name: "Platform Team"},
	})
	if byID["S1"] != "platform-team" {
		t.Errorf("name fallback = %q, want platform-team (no spaces)", byID["S1"])
	}
}

func TestUsergroupHandlesSkipsUnmentionableGroups(t *testing.T) {
	byID := usergroupHandles([]slack.UserGroup{
		{ID: "S1"},
		{ID: "S2", Name: "   "},
		{ID: "S3", Name: "!!!"},
	})
	if len(byID) != 0 {
		t.Errorf("byID = %v, want empty (nothing mentionable)", byID)
	}
}

func TestSlugifyHandle(t *testing.T) {
	cases := map[string]string{
		"Platform Team":     "platform-team",
		"  Data   Eng  ":    "data-eng",
		"Design/Research":   "design-research",
		"on-call_rota.v2":   "on-call_rota.v2",
		"Team (EU) — North": "team-eu-north",
		"":                  "",
		"???":               "",
	}
	for in, want := range cases {
		if got := slugifyHandle(in); got != want {
			t.Errorf("slugifyHandle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorkspaceContextUserGroupsDefaultsEmpty(t *testing.T) {
	var wctx WorkspaceContext
	if got := wctx.UserGroups(); got == nil || len(got) != 0 {
		t.Errorf("UserGroups() before load = %v, want empty non-nil map", got)
	}
}

// The usergroups.list fetch publishes the map from a background
// goroutine while the RTM event loop and bubbletea cmds read it. Run
// under -race to catch a regression back to a plain map field.
func TestWorkspaceContextUserGroupsConcurrentAccess(t *testing.T) {
	wctx := &WorkspaceContext{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			wctx.SetUserGroups(map[string]string{"S1": "platform-team"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = wctx.UserGroups()["S1"]
		}
	}()
	wg.Wait()
	if got := wctx.UserGroups()["S1"]; got != "platform-team" {
		t.Errorf("UserGroups()[S1] = %q, want platform-team", got)
	}
}

func TestSelfSubteamIDs(t *testing.T) {
	groups := []slack.UserGroup{
		{ID: "S0ENG", Handle: "eng", Users: []string{"U1", "U2"}},
		{ID: "S0QA", Handle: "qa", Users: []string{"U3"}},
		{ID: "S0PLATFORM", Handle: "platform", Users: []string{"U2", "U1"}},
	}
	set := selfSubteamIDs(groups, "U1")
	if _, ok := set["S0ENG"]; !ok {
		t.Errorf("selfSubteamIDs missing S0ENG; got %v", set)
	}
	if _, ok := set["S0PLATFORM"]; !ok {
		t.Errorf("selfSubteamIDs missing S0PLATFORM; got %v", set)
	}
	if _, ok := set["S0QA"]; ok {
		t.Errorf("selfSubteamIDs should not include S0QA (U1 not a member); got %v", set)
	}
}

func TestSelfSubteamIDs_EmptyUserID(t *testing.T) {
	groups := []slack.UserGroup{{ID: "S0ENG", Users: []string{"U1"}}}
	set := selfSubteamIDs(groups, "")
	if len(set) != 0 {
		t.Errorf("selfSubteamIDs with empty userID = %v, want empty", set)
	}
}

func TestSelfSubteamIDs_NoMembership(t *testing.T) {
	groups := []slack.UserGroup{{ID: "S0ENG", Users: []string{"U2"}}}
	set := selfSubteamIDs(groups, "U1")
	if len(set) != 0 {
		t.Errorf("selfSubteamIDs with no matching membership = %v, want empty", set)
	}
}

func TestWorkspaceContextSelfSubteamsDefaultsEmpty(t *testing.T) {
	var wctx WorkspaceContext
	if got := wctx.SelfSubteams(); got == nil || len(got) != 0 {
		t.Errorf("SelfSubteams() before load = %v, want empty non-nil map", got)
	}
}

func TestWorkspaceContextSelfSubteamsRoundTrip(t *testing.T) {
	wctx := &WorkspaceContext{}
	set := map[string]struct{}{"S0ENG": {}}
	wctx.SetSelfSubteams(set)
	if _, ok := wctx.SelfSubteams()["S0ENG"]; !ok {
		t.Errorf("SelfSubteams() after SetSelfSubteams = %v, want S0ENG present", wctx.SelfSubteams())
	}
}

// The selfSubteams map is published from the background usergroups.list
// fetch goroutine while the RTM event loop reads it. Run under -race to
// catch a regression back to a plain map field.
func TestWorkspaceContextSelfSubteamsConcurrentAccess(t *testing.T) {
	wctx := &WorkspaceContext{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			wctx.SetSelfSubteams(map[string]struct{}{"S1": {}})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = wctx.SelfSubteams()["S1"]
		}
	}()
	wg.Wait()
	if _, ok := wctx.SelfSubteams()["S1"]; !ok {
		t.Errorf("SelfSubteams()[S1] missing after concurrent writes; got %v", wctx.SelfSubteams())
	}
}
