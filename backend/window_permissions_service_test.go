package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"fiatjaf.com/nostr"
)

func servicePermissionFixture(t *testing.T) (Napp, Napp) {
	t.Helper()
	resetLauncherState(t)
	first := Napp{D: "first", Author: nostr.Generate().Public(), Format: FormatNapplet,
		RequiredDomains: []string{"relay.example"}, OptionalDomains: []string{"media.example"}}
	second := Napp{D: "second", Author: nostr.Generate().Public(), Format: FormatNapplet}
	first.ID, second.ID = first.Address(), second.Address()
	stateMu.Lock()
	state.InstalledNapps = map[string]Napp{first.ID: first, second.ID: second}
	state.Rules = map[string]Rule{}
	stateMu.Unlock()
	return first, second
}

func TestServicePermissionsGet(t *testing.T) {
	first, second := servicePermissionFixture(t)
	stateMu.Lock()
	state.Rules[RuleKey{Napp: first.ID, Permission: PermSign}.ruleID()] = Rule{Decision: DecisionDeny}
	state.Rules[RuleKey{Napp: first.ID, Permission: PermDispatch, Subject: "view"}.ruleID()] = Rule{Decision: DecisionAllow}
	state.Rules[RuleKey{Napp: second.ID, Permission: PermSign}.ruleID()] = Rule{Decision: DecisionAllow}
	stateMu.Unlock()
	sessionKey := RuleKey{Napp: first.ID, Permission: PermFetch}
	setSessionRule(sessionKey, Rule{Decision: DecisionAllow})
	t.Cleanup(func() { clearSessionRule(sessionKey) })
	got, err := ServicePermissionsGet(context.Background(), first.Address())
	if err != nil || got.Address != first.Address() ||
		!reflect.DeepEqual(got.RequiredDomains, []string{"relay.example"}) ||
		!reflect.DeepEqual(got.OptionalDomains, []string{"media.example"}) ||
		!reflect.DeepEqual(got.SavedRules, []ServiceSavedRule{
			{Permission: PermDispatch, Subject: "view", Decision: DecisionAllow},
			{Permission: PermSign, Decision: DecisionDeny},
		}) {
		t.Fatalf("permission view: %+v %v", got, err)
	}
	if _, err := ServicePermissionsGet(context.Background(), "private-id"); !errors.Is(err, ErrServiceInvalidAddress) {
		t.Fatalf("internal ID accepted: %v", err)
	}
}
