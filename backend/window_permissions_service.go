package backend

import (
	"context"
	"slices"
	"sort"
)

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
