package backend

import (
	"context"
	"errors"
	"slices"
	"sort"
	"unicode"
	"unicode/utf8"
)

var ErrServiceInvalidPermission = errors.New("invalid service permission")

// ServiceSavedRule is a persisted decision scoped to the requested address.
// The internal napplet ID is intentionally absent from the socket DTO.
type ServiceSavedRule struct {
	Permission Permission `json:"permission"`
	Subject    string     `json:"subject"`
	Decision   Decision   `json:"decision"`
}

type ServicePermissionsResult struct {
	Address         string             `json:"address"`
	RequiredDomains []string           `json:"required_domains"`
	OptionalDomains []string           `json:"optional_domains"`
	SavedRules      []ServiceSavedRule `json:"saved_rules"`
}

type ServicePermissionResult struct {
	Address    string     `json:"address"`
	Permission Permission `json:"permission"`
	Subject    string     `json:"subject"`
	Decision   Decision   `json:"decision,omitempty"`
	Cleared    *bool      `json:"cleared,omitempty"`
}

func servicePermissionNappLocked(address string) (string, Napp, bool) {
	for id, n := range state.InstalledNapps {
		if n.IsNapplet() && n.Address() == address {
			return id, n, true
		}
	}
	return "", Napp{}, false
}

func ServicePermissionsGet(ctx context.Context, address string) (ServicePermissionsResult, error) {
	if _, err := ParseCanonicalServiceAddress(address); err != nil {
		return ServicePermissionsResult{}, err
	}
	if ctx.Err() != nil {
		return ServicePermissionsResult{}, ErrServiceTimeout
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	id, n, ok := servicePermissionNappLocked(address)
	if !ok {
		return ServicePermissionsResult{}, ErrServiceNotFound
	}
	result := ServicePermissionsResult{
		Address: address, RequiredDomains: slices.Clone(n.RequiredDomains),
		OptionalDomains: slices.Clone(n.OptionalDomains), SavedRules: []ServiceSavedRule{},
	}
	if result.RequiredDomains == nil {
		result.RequiredDomains = []string{}
	}
	if result.OptionalDomains == nil {
		result.OptionalDomains = []string{}
	}
	for raw, rule := range state.Rules {
		key := ruleKeyFromID(raw)
		if key.Napp == id && (rule.Decision == DecisionAllow || rule.Decision == DecisionDeny) {
			result.SavedRules = append(result.SavedRules, ServiceSavedRule{key.Permission, key.Subject, rule.Decision})
		}
	}
	sort.Slice(result.SavedRules, func(i, j int) bool {
		a, b := result.SavedRules[i], result.SavedRules[j]
		if a.Permission != b.Permission {
			return a.Permission < b.Permission
		}
		return a.Subject < b.Subject
	})
	return result, nil
}

// ValidServicePermission checks the public rule vocabulary and subject shape.
func ValidServicePermission(perm Permission, subject string) bool {
	switch perm {
	case PermSign, PermEncrypt, PermDecrypt, PermPublish, PermOpenLink,
		PermSaveFile, PermCopyText, PermUpload, PermFetch, PermNotify, PermMedia, PermDispatch:
	default:
		return false
	}
	if (perm == PermDispatch && subject == "") || len(subject) > 256 || !utf8.ValidString(subject) {
		return false
	}
	for _, r := range subject {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func ServicePermissionSet(ctx context.Context, address string, perm Permission, subject string, decision Decision) (ServicePermissionResult, error) {
	if _, err := ParseCanonicalServiceAddress(address); err != nil {
		return ServicePermissionResult{}, err
	}
	if !ValidServicePermission(perm, subject) || (decision != DecisionAllow && decision != DecisionDeny) {
		return ServicePermissionResult{}, ErrServiceInvalidPermission
	}
	if ctx.Err() != nil {
		return ServicePermissionResult{}, ErrServiceTimeout
	}
	stateMu.Lock()
	id, _, ok := servicePermissionNappLocked(address)
	if !ok {
		stateMu.Unlock()
		return ServicePermissionResult{}, ErrServiceNotFound
	}
	key := RuleKey{Napp: id, Permission: perm, Subject: subject}.ruleID()
	previous, hadPrevious := state.Rules[key]
	if perm == PermDispatch && decision == DecisionAllow && previous.Target == "" {
		stateMu.Unlock()
		return ServicePermissionResult{}, ErrServiceInvalidPermission
	}
	if hadPrevious && previous.Decision == decision {
		stateMu.Unlock()
		return ServicePermissionResult{Address: address, Permission: perm, Subject: subject, Decision: decision}, nil
	}
	if state.Rules == nil {
		state.Rules = make(map[string]Rule)
	}
	// Retain an existing dispatch target when changing its decision. The
	// socket cannot select a handler target, so it cannot invent one.
	state.Rules[key] = Rule{Decision: decision, Target: previous.Target}
	if err := saveState(); err != nil {
		if hadPrevious {
			state.Rules[key] = previous
		} else {
			delete(state.Rules, key)
		}
		stateMu.Unlock()
		return ServicePermissionResult{}, ErrServiceUnavailable
	}
	stateMu.Unlock()
	notifyState()
	if perm == PermDispatch {
		backgroundSyncs.Go(broadcastIntentChanges)
	}
	return ServicePermissionResult{Address: address, Permission: perm, Subject: subject, Decision: decision}, nil
}

func ServicePermissionClear(ctx context.Context, address string, perm Permission, subject string) (ServicePermissionResult, error) {
	if _, err := ParseCanonicalServiceAddress(address); err != nil {
		return ServicePermissionResult{}, err
	}
	if !ValidServicePermission(perm, subject) {
		return ServicePermissionResult{}, ErrServiceInvalidPermission
	}
	if ctx.Err() != nil {
		return ServicePermissionResult{}, ErrServiceTimeout
	}
	stateMu.Lock()
	id, _, ok := servicePermissionNappLocked(address)
	if !ok {
		stateMu.Unlock()
		return ServicePermissionResult{}, ErrServiceNotFound
	}
	key := RuleKey{Napp: id, Permission: perm, Subject: subject}.ruleID()
	previous, existed := state.Rules[key]
	if !existed {
		stateMu.Unlock()
		cleared := false
		return ServicePermissionResult{Address: address, Permission: perm, Subject: subject, Cleared: &cleared}, nil
	}
	delete(state.Rules, key)
	if err := saveState(); err != nil {
		state.Rules[key] = previous
		stateMu.Unlock()
		return ServicePermissionResult{}, ErrServiceUnavailable
	}
	stateMu.Unlock()
	notifyState()
	if perm == PermDispatch {
		backgroundSyncs.Go(broadcastIntentChanges)
	}
	cleared := true
	return ServicePermissionResult{Address: address, Permission: perm, Subject: subject, Cleared: &cleared}, nil
}
