package backend

import (
	"maps"
	"slices"
	"sort"
	"strings"
)

// Catalog is built from installed, verified napplet records. Development
// napplets are excluded: their identity is not backed by a verified manifest.
type catalogIdentity struct {
	DTag          string `json:"dTag"`
	AggregateHash string `json:"aggregateHash"`
}

type catalogParameter struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

type catalogIntent struct {
	Intent     string             `json:"intent"`
	Convention string             `json:"convention"`
	Parameters []catalogParameter `json:"parameters"`
}

type catalogArchetype struct {
	Archetype string          `json:"archetype"`
	Intents   []catalogIntent `json:"intents"`
}

type catalogNapplet struct {
	Identity    catalogIdentity    `json:"identity"`
	Title       string             `json:"title,omitempty"`
	Description string             `json:"description,omitempty"`
	Requires    []string           `json:"requires"`
	Archetypes  []catalogArchetype `json:"archetypes"`
}

type catalogHandler struct {
	Archetype      string           `json:"archetype"`
	CurrentHandler *catalogIdentity `json:"currentHandler"`
}

type catalogSnapshot struct {
	Napplets []catalogNapplet `json:"napplets"`
	Handlers []catalogHandler `json:"handlers"`
}

func init() { handleNap(map[string]napHandler{"catalog.get": napCatalogGet}) }

func napCatalogGet(c *napCall) {
	c.reply(map[string]any{"snapshot": buildCatalogSnapshot(installedNapps())})
}

func buildCatalogSnapshot(installed []Napp) catalogSnapshot {
	snapshot := catalogSnapshot{Napplets: []catalogNapplet{}, Handlers: []catalogHandler{}}
	byRole := map[string][]Napp{}
	// Stable output independent of map iteration and launcher display ordering.
	installed = slices.Clone(installed)
	sort.Slice(installed, func(i, j int) bool { return installed[i].Address() < installed[j].Address() })
	for _, n := range installed {
		if !n.IsNapplet() || n.Unavailable != "" || !hex64.MatchString(n.ArtifactHash) {
			continue
		}
		identity := catalogIdentity{DTag: n.D, AggregateHash: n.ArtifactHash}
		entry := catalogNapplet{Identity: identity, Title: n.Name, Description: n.Description,
			Requires: slices.Clone(n.RequiredDomains), Archetypes: []catalogArchetype{}}
		if entry.Requires == nil {
			entry.Requires = []string{}
		}
		roles := slices.Clone(n.Roles)
		for _, c := range n.Conventions {
			roles = appendUniqueString(roles, conventionRole(c.ID))
		}
		sort.Strings(roles)
		for _, role := range roles {
			archetype := catalogArchetype{Archetype: role, Intents: []catalogIntent{}}
			for _, c := range n.Conventions {
				if conventionRole(c.ID) != role {
					continue
				}
				archetype.Intents = append(archetype.Intents, catalogIntent{
					Intent: strings.TrimPrefix(c.ID, "napplet:"+role+"/"), Convention: c.ID, Parameters: []catalogParameter{},
				})
			}
			sort.Slice(archetype.Intents, func(i, j int) bool { return archetype.Intents[i].Convention < archetype.Intents[j].Convention })
			entry.Archetypes = append(entry.Archetypes, archetype)
			if n.Handles("napplet:" + role + "/open") {
				byRole[role] = append(byRole[role], n)
			} else if _, exists := byRole[role]; !exists {
				byRole[role] = nil
			}
		}
		snapshot.Napplets = append(snapshot.Napplets, entry)
	}
	for _, role := range slices.Sorted(maps.Keys(byRole)) {
		candidates := byRole[role]
		var current *catalogIdentity
		if len(candidates) == 1 {
			current = &catalogIdentity{DTag: candidates[0].D, AggregateHash: candidates[0].ArtifactHash}
		} else if rule, ok := lookupRule(intentDefaultKey(role)); ok && rule.Decision.granted() {
			for _, n := range candidates {
				if n.ID == rule.Target {
					current = &catalogIdentity{DTag: n.D, AggregateHash: n.ArtifactHash}
					break
				}
			}
		}
		snapshot.Handlers = append(snapshot.Handlers, catalogHandler{Archetype: role, CurrentHandler: current})
	}
	return snapshot
}
