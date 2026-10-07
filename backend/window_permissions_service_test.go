package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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

func TestServicePermissionsMutateExactRuleAndPersist(t *testing.T) {
	first, second := servicePermissionFixture(t)
	otherSubject := RuleKey{Napp: first.ID, Permission: PermDispatch, Subject: "view"}
	otherApp := RuleKey{Napp: second.ID, Permission: PermSign}
	stateMu.Lock()
	state.Rules[otherSubject.ruleID()] = Rule{Decision: DecisionDeny}
	state.Rules[otherApp.ruleID()] = Rule{Decision: DecisionDeny}
	stateMu.Unlock()
	sessionKey := RuleKey{Napp: first.ID, Permission: PermSign, Subject: "session"}
	setSessionRule(sessionKey, Rule{Decision: DecisionAllow})
	t.Cleanup(func() { clearSessionRule(sessionKey) })
	result, err := ServicePermissionSet(context.Background(), first.Address(), PermSign, "saved", DecisionAllow)
	if err != nil || result.Decision != DecisionAllow {
		t.Fatalf("set: %+v %v", result, err)
	}
	var persisted AppState
	data, err := os.ReadFile(statePath)
	if err != nil || json.Unmarshal(data, &persisted) != nil {
		t.Fatalf("saved state: %v", err)
	}
	key := RuleKey{Napp: first.ID, Permission: PermSign, Subject: "saved"}
	if persisted.Rules[key.ruleID()].Decision != DecisionAllow {
		t.Fatalf("missing persisted allow: %+v", persisted.Rules)
	}
	if _, err := ServicePermissionClear(context.Background(), first.Address(), PermSign, "saved"); err != nil {
		t.Fatal(err)
	}
	if _, ok := storedRule(key); ok {
		t.Fatal("exact key remained after clear")
	}
	if _, ok := storedRule(otherSubject); !ok {
		t.Fatal("other subject removed")
	}
	if _, ok := storedRule(otherApp); !ok {
		t.Fatal("other app removed")
	}
	if _, ok := sessionRule(sessionKey); !ok {
		t.Fatal("session consent removed")
	}
}

func TestServicePermissionsRejectAndRollback(t *testing.T) {
	first, _ := servicePermissionFixture(t)
	for _, tc := range []struct {
		perm     Permission
		subject  string
		decision Decision
	}{
		{"invented", "", DecisionAllow}, {PermDispatch, "", DecisionDeny},
		{PermSign, "bad\x00subject", DecisionAllow}, {PermSign, "", DecisionAsk},
	} {
		if _, err := ServicePermissionSet(context.Background(), first.Address(), tc.perm, tc.subject, tc.decision); !errors.Is(err, ErrServiceInvalidPermission) {
			t.Fatalf("accepted invalid %q/%q/%q: %v", tc.perm, tc.subject, tc.decision, err)
		}
	}
	if _, err := ServicePermissionSet(context.Background(), "not-an-address", PermSign, "", DecisionAllow); !errors.Is(err, ErrServiceInvalidAddress) {
		t.Fatalf("accepted internal ID: %v", err)
	}
	key := RuleKey{Napp: first.ID, Permission: PermSign}
	stateSaveBlocked.Store(true)
	t.Cleanup(func() { stateSaveBlocked.Store(false) })
	if _, err := ServicePermissionSet(context.Background(), first.Address(), PermSign, "", DecisionAllow); !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("failed save returned: %v", err)
	}
	if _, ok := storedRule(key); ok {
		t.Fatal("failed set remained in memory")
	}
	stateMu.Lock()
	state.Rules[key.ruleID()] = Rule{Decision: DecisionDeny}
	stateMu.Unlock()
	if _, err := ServicePermissionClear(context.Background(), first.Address(), PermSign, ""); !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("failed clear returned: %v", err)
	}
	if r, ok := storedRule(key); !ok || r.Decision != DecisionDeny {
		t.Fatalf("failed clear did not roll back: %+v %v", r, ok)
	}
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
