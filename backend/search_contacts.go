package backend

import (
	"context"

	"fiatjaf.com/nostr"
)

// FollowedAuthors returns the current user's follow list for host-side
// discovery integrations. Callers should refresh it away from interactive
// search paths because the SDK may consult relays when its cache is cold.
// The user's own key is included so their napplets remain discoverable too.
func FollowedAuthors(ctx context.Context) []nostr.PubKey {
	pk, ok := currentUser()
	if !ok || sys == nil {
		return nil
	}
	list := sys.FetchFollowList(ctx, pk)
	authors := make([]nostr.PubKey, 0, len(list.Items)+1)
	authors = append(authors, pk)
	for _, item := range list.Items {
		authors = append(authors, item.Pubkey)
	}
	return authors
}
