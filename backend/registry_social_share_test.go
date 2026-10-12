package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/nip19"
)

// shareEntity decodes the nostr: URI at the end of a share's content.
func shareEntity(t *testing.T, content string) (string, any) {
	t.Helper()
	i := strings.LastIndex(content, "nostr:")
	if i < 0 {
		t.Fatalf("no nostr: URI in %q", content)
	}
	code := content[i+len("nostr:"):]
	prefix, value, err := nip19.Decode(code)
	if err != nil {
		t.Fatalf("%s does not decode: %v", code, err)
	}
	return prefix, value
}

func oneShare(t *testing.T, got []nostr.Event) nostr.Event {
	t.Helper()
	if len(got) != 1 || got[0].Kind != nostr.KindTextNote {
		t.Fatalf("relay got %v", got)
	}
	return got[0]
}

// ─── tags and the entity ─────────────────────────────────────────

func TestSocialShareNapplet(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	pk := fx.author.Public()
	res, err := ServiceSocialShare(context.Background(), fx.address, "Try this")
	if err != nil {
		t.Fatal(err)
	}
	evt := oneShare(t, fx.got())
	wantTags(t, "share", evt.Tags, nostr.Tags{{"q", fx.address}, {"p", pk.Hex()}, {"a", fx.address}})
	if !strings.HasPrefix(evt.Content, "Try this\n\nnostr:naddr1") || strings.Count(evt.Content, "nostr:") != 1 {
		t.Fatalf("content %q", evt.Content)
	}
	prefix, value := shareEntity(t, evt.Content)
	ptr, ok := value.(nostr.EntityPointer)
	if prefix != "naddr" || !ok || ptr.PublicKey != pk || ptr.Kind != KindNapplet || ptr.Identifier != "app" || len(ptr.Relays) != 0 {
		t.Fatalf("decoded %s %+v", prefix, value)
	}
	want := ServiceSocialPublishResult{Address: fx.address, Outcome: "published", EventID: evt.ID.Hex(), Published: 1}
	if res != want {
		t.Fatalf("result %+v, want %+v", res, want)
	}
	if !stored(evt.ID) || evt.PubKey != fx.me.Public() {
		t.Fatal("the share was not kept locally")
	}
	// it shows at once as the user's own quote
	page, err := ServiceMentions(context.Background(), fx.address, 0, 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != res.EventID || !page.Items[0].Quote {
		t.Fatalf("mentions %+v %v", page, err)
	}
	summary, err := ServiceSocial(context.Background(), fx.address, false, nil)
	if err != nil || summary.Mentions.Count != 1 {
		t.Fatalf("summary %+v %v", summary.Mentions, err)
	}
}

func TestSocialShareRelayHint(t *testing.T) {
	fx := newPublishFixture(t, "wss://hint.example", false)
	pk := fx.author.Public()
	if _, err := ServiceSocialShare(context.Background(), fx.address, "Try this"); err != nil {
		t.Fatal(err)
	}
	evt := oneShare(t, fx.got())
	wantTags(t, "hinted share", evt.Tags, nostr.Tags{{"q", fx.address, "wss://hint.example"}, {"p", pk.Hex()}, {"a", fx.address}})
	_, value := shareEntity(t, evt.Content)
	if ptr, ok := value.(nostr.EntityPointer); !ok || !slices.Equal(ptr.Relays, []string{"wss://hint.example"}) {
		t.Fatalf("decoded %+v", value)
	}
}

func TestSocialShareRootNapplet(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	pk := fx.author.Public()
	root := "15129:" + pk.Hex() + ":"
	if _, err := ServiceSocialShare(context.Background(), root, ""); err != nil {
		t.Fatal(err)
	}
	evt := oneShare(t, fx.got())
	wantTags(t, "15129 share", evt.Tags, nostr.Tags{{"q", root}, {"p", pk.Hex()}, {"a", root}})
	prefix, value := shareEntity(t, evt.Content)
	ptr, ok := value.(nostr.EntityPointer)
	if prefix != "naddr" || !ok || ptr.Kind != KindRootNapplet || ptr.Identifier != "" || ptr.PublicKey != pk {
		t.Fatalf("decoded %s %+v", prefix, value)
	}
	if rebuilt := fmt.Sprintf("%d:%s:%s", ptr.Kind, ptr.PublicKey.Hex(), ptr.Identifier); rebuilt != root {
		t.Fatalf("rebuilt %q, want %q", rebuilt, root)
	}
}

func TestSocialShareSnapshot(t *testing.T) {
	for _, hint := range []string{"", "wss://hint.example"} {
		t.Run("hint="+hint, func(t *testing.T) {
			fx := newPublishFixture(t, hint, false)
			pk := fx.author.Public()
			id := strings.Repeat("ab", 32)
			snapshot := "5129:" + pk.Hex() + ":" + id
			if _, err := ServiceSocialShare(context.Background(), snapshot, "a snapshot"); err != nil {
				t.Fatal(err)
			}
			evt := oneShare(t, fx.got())
			wantTags(t, "5129 share", evt.Tags, nostr.Tags{{"q", id, hint, pk.Hex()}, {"p", pk.Hex()}})
			if !strings.HasPrefix(evt.Content, "a snapshot\n\nnostr:nevent1") {
				t.Fatalf("content %q", evt.Content)
			}
			prefix, value := shareEntity(t, evt.Content)
			ptr, ok := value.(nostr.EventPointer)
			if prefix != "nevent" || !ok || ptr.ID.Hex() != id || ptr.Author != pk {
				t.Fatalf("decoded %s %+v", prefix, value)
			}
			var wantRelays []string
			if hint != "" {
				wantRelays = []string{hint}
			}
			if !slices.Equal(ptr.Relays, wantRelays) {
				t.Fatalf("relays %v, want %v", ptr.Relays, wantRelays)
			}
			// the snapshot's share is its own quote mention
			page, err := ServiceMentions(context.Background(), snapshot, 0, 10)
			if err != nil || len(page.Items) != 1 || !page.Items[0].Quote {
				t.Fatalf("mentions %+v %v", page, err)
			}
		})
	}
}

// ─── content rules ───────────────────────────────────────────────

func TestSocialShareContent(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	ctx := context.Background()
	entity, ok := socialShareEntity(socialTarget{Address: fx.address, Author: fx.author.Public(), Kind: KindNapplet}, "")
	if !ok {
		t.Fatal("no entity")
	}
	uri := "nostr:" + entity
	for i, tc := range []struct{ text, want string }{
		{"", uri},
		{" \n\t", uri},
		{"hi  \n\n", "hi\n\n" + uri},
		{"  lead\ninner\n\nlines \t", "  lead\ninner\n\nlines\n\n" + uri},
	} {
		if _, err := ServiceSocialShare(ctx, fx.address, tc.text); err != nil {
			t.Fatal(err)
		}
		got := fx.got()
		if len(got) != i+1 || got[i].Content != tc.want {
			t.Fatalf("%q published %q, want %q", tc.text, got[len(got)-1].Content, tc.want)
		}
	}
}

func TestSocialShareLimits(t *testing.T) {
	// the longest share: a 255-byte d tag, a 255-byte hint and the most text
	hint := "wss://" + strings.Repeat("h", socialNIP19Value-len("wss://"))
	fx := newPublishFixture(t, hint, false)
	ctx := context.Background()
	pk := fx.author.Public()
	longest := nappletAddress(pk, strings.Repeat("d", socialNIP19Value))
	if _, err := ServiceSocialShare(ctx, longest, strings.Repeat("x", socialShareContentMax)); err != nil {
		t.Fatal(err)
	}
	evt := oneShare(t, fx.got())
	if len(evt.Content) > socialMaxContent {
		t.Fatalf("content is %d bytes", len(evt.Content))
	}
	if _, value := shareEntity(t, evt.Content); value.(nostr.EntityPointer).Identifier != strings.Repeat("d", socialNIP19Value) ||
		!slices.Equal(value.(nostr.EntityPointer).Relays, []string{hint}) {
		t.Fatalf("decoded %+v", value)
	}
	// every Kwakore reader keeps it
	target, err := newSocialTarget(longest)
	if err != nil || !socialIngestable(evt, target, time.Now()) {
		t.Fatalf("the longest share is not ingestable: %v", err)
	}

	for name, call := range map[string]func() error{
		"text too long": func() error {
			_, err := ServiceSocialShare(ctx, fx.address, strings.Repeat("x", socialShareContentMax+1))
			return err
		},
		"d tag too long": func() error {
			_, err := ServiceSocialShare(ctx, nappletAddress(pk, strings.Repeat("d", socialNIP19Value+1)), "hi")
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrServiceInvalidSocial) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := len(fx.got()); n != 1 {
		t.Fatalf("a refused share published (%d events)", n)
	}
}

func TestSocialShareDropsLongHint(t *testing.T) {
	hint := "wss://" + strings.Repeat("h", socialNIP19Value)
	fx := newPublishFixture(t, hint, false)
	if _, err := ServiceSocialShare(context.Background(), fx.address, "hi"); err != nil {
		t.Fatal(err)
	}
	evt := oneShare(t, fx.got())
	wantTags(t, "long hint share", evt.Tags, nostr.Tags{{"q", fx.address}, {"p", fx.author.Public().Hex()}, {"a", fx.address}})
	if _, value := shareEntity(t, evt.Content); len(value.(nostr.EntityPointer).Relays) != 0 {
		t.Fatalf("decoded %+v", value)
	}
}

// ─── share, then unpublish ───────────────────────────────────────

func TestSocialShareThenDelete(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	ctx := context.Background()
	share, err := ServiceSocialShare(ctx, fx.address, "Try this")
	if err != nil {
		t.Fatal(err)
	}
	foreign := saved(t, signedWith(t, nostr.Generate(), nostr.KindTextNote, nostr.Tags{{"a", fx.address}}, "theirs", nostr.Now()-10))
	unrelated := saved(t, signedWith(t, fx.me, nostr.KindTextNote, nostr.Tags{{"t", "x"}}, "mine, unrelated", nostr.Now()-10))
	other := nappletAddress(fx.author.Public(), "other")
	otherShare, err := ServiceSocialShare(ctx, other, "the other one")
	if err != nil {
		t.Fatal(err)
	}
	sent := len(fx.got())
	for name, id := range map[string]string{
		"foreign": foreign.ID.Hex(), "unrelated": unrelated.ID.Hex(), "other address": otherShare.EventID,
	} {
		if _, err := ServiceSocialDelete(ctx, fx.address, id); !errors.Is(err, ErrServiceNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := len(fx.got()); n != sent {
		t.Fatalf("a refused deletion published (%d events)", n-sent)
	}

	res, err := ServiceSocialDelete(ctx, fx.address, share.EventID)
	if err != nil {
		t.Fatal(err)
	}
	got := fx.got()
	if len(got) != sent+1 {
		t.Fatalf("relay got %d events", len(got))
	}
	deletion := got[sent]
	wantTags(t, "share deletion", deletion.Tags, nostr.Tags{{"e", share.EventID}, {"k", "1"}})
	if res.Outcome != "published" || res.DeletionID != deletion.ID.Hex() || res.Published != 1 {
		t.Fatalf("result %+v", res)
	}
	id, err := nostr.IDFromHex(share.EventID)
	if err != nil {
		t.Fatal(err)
	}
	shareEvt := nostr.Event{ID: id, PubKey: fx.me.Public()}
	if stored(id) || !socialTombstoned(sys.Store, shareEvt) {
		t.Fatal("the share was not removed with a tombstone")
	}
	summary, err := ServiceSocial(ctx, fx.address, false, nil)
	if err != nil || summary.Mentions.Count != 1 {
		t.Fatalf("mentions after unpublish %+v %v", summary.Mentions, err)
	}
	if !stored(foreign.ID) || !stored(unrelated.ID) {
		t.Fatal("an event not deleted was removed")
	}
}

// ─── signer failures ─────────────────────────────────────────────

func TestSocialShareSigner(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	ctx := context.Background()
	if _, err := ServiceSocialShare(ctx, "bad", "hi"); !errors.Is(err, ErrServiceInvalidAddress) {
		t.Fatalf("bad address: %v", err)
	}

	withUserKeyer(t, nil, nostr.ZeroPK)
	if _, err := ServiceSocialShare(ctx, fx.address, "hi"); !errors.Is(err, ErrServiceSignerUnavailable) {
		t.Fatalf("no signer: %v", err)
	}

	withUserKeyer(t, stallKeyer{keyer.NewPlainKeySigner(fx.me)}, fx.me.Public())
	socialSignTimeout = 50 * time.Millisecond
	if _, err := ServiceSocialShare(ctx, fx.address, "hi"); !errors.Is(err, ErrServiceTimeout) {
		t.Fatalf("stalled signer: %v", err)
	}

	withUserKeyer(t, keyer.NewPlainKeySigner(nostr.Generate()), fx.me.Public())
	if _, err := ServiceSocialShare(ctx, fx.address, "hi"); !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("wrong key: %v", err)
	}
	if n := len(fx.got()); n != 0 {
		t.Fatalf("published %d events", n)
	}
	if ids := mentionsStored(t); len(ids) != 0 {
		t.Fatalf("stored %v", ids)
	}
}

// ─── wire hygiene ────────────────────────────────────────────────

func TestSocialShareResultsCarryNoRelays(t *testing.T) {
	fx := newPublishFixture(t, "wss://hint.example", false)
	ctx := context.Background()
	text := "a distinctive shared text"
	share, err := ServiceSocialShare(ctx, fx.address, text)
	if err != nil {
		t.Fatal(err)
	}
	del, err := ServiceSocialDelete(ctx, fx.address, share.EventID)
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		res  any
		keys []string
	}{
		{share, []string{"address", "event_id", "failed", "outcome", "published"}},
		{del, []string{"address", "deletion_id", "failed", "outcome", "published"}},
	} {
		raw, err := json.Marshal(tc.res)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"ws://", "wss://", "127.0.0.1", "hint.example", "relays", "/", "nostr:", "naddr", "nevent", text} {
			if strings.Contains(string(raw), marker) {
				t.Errorf("result %d carries %q: %s", i, marker, raw)
			}
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		var got []string
		for k := range m {
			got = append(got, k)
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.keys) {
			t.Errorf("result %d keys %v, want %v", i, got, tc.keys)
		}
	}
}
