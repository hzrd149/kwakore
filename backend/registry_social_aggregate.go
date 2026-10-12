package backend

import (
	"cmp"
	"slices"

	"fiatjaf.com/nostr"
)

// Pure aggregation of napplet social events: no store, network, globals or
// clock. Every input is already post-filtered to events that name the target
// exactly (socialMatches); the functions still skip anything that does not,
// so a careless caller cannot widen what is counted.

const (
	// socialEmojiLimit caps the emoji and by_event lists of a summary, and
	// the viewer's own reactions.
	socialEmojiLimit = 20
)

type socialDeletionKey struct {
	author nostr.PubKey
	id     string
}

// socialLive drops every event a deletion names, but only a deletion by the
// event's own author counts (NIP-09).
func socialLive(events, deletions []nostr.Event) []nostr.Event {
	deleted := make(map[socialDeletionKey]bool)
	for _, d := range deletions {
		if d.Kind != nostr.KindDeletion {
			continue
		}
		for _, tag := range d.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				deleted[socialDeletionKey{d.PubKey, tag[1]}] = true
			}
		}
	}
	out := make([]nostr.Event, 0, len(events))
	for _, evt := range events {
		if !deleted[socialDeletionKey{evt.PubKey, evt.ID.Hex()}] {
			out = append(out, evt)
		}
	}
	return out
}

// isVoteContent says whether a reaction's content is a vote: "+" or ""
// (both up, per NIP-25) or "-".
func isVoteContent(c string) bool { return c == "+" || c == "" || c == "-" }

func isUpVote(c string) bool { return c == "+" || c == "" }

// latestVotes is each author's NIP-01 newest vote reaction: the latest
// created_at, and on a tie the lowest id.
func latestVotes(reactions []nostr.Event) map[nostr.PubKey]nostr.Event {
	out := make(map[nostr.PubKey]nostr.Event)
	for _, evt := range reactions {
		if evt.Kind != nostr.KindReaction || !isVoteContent(evt.Content) {
			continue
		}
		if cur, ok := out[evt.PubKey]; !ok || newerEvent(evt, cur) {
			out[evt.PubKey] = evt
		}
	}
	return out
}

// socialOrder sorts events newest first: created_at desc, then id asc.
func socialOrder(a, b nostr.Event) int {
	switch {
	case a.ID == b.ID:
		return 0
	case newerEvent(a, b):
		return -1
	default:
		return 1
	}
}

// socialShortcode is a :shortcode: reaction's name, or "".
func socialShortcode(content string) string {
	if m := shortcodeRe.FindStringSubmatch(content); m != nil {
		return m[1]
	}
	return ""
}

// socialEmojiTagURL is the sanitized url of evt's NIP-30 emoji tag for
// shortcode, or "".
func socialEmojiTagURL(evt nostr.Event, shortcode string) string {
	if shortcode == "" {
		return ""
	}
	for _, tag := range evt.Tags {
		if len(tag) >= 3 && tag[0] == "emoji" && tag[1] == shortcode {
			if u := socialEmojiURL(tag[2]); u != "" {
				return u
			}
		}
	}
	return ""
}

func socialReactionsOf(t socialTarget, events []nostr.Event) []nostr.Event {
	out := make([]nostr.Event, 0, len(events))
	for _, evt := range events {
		if evt.Kind == nostr.KindReaction && socialMatches(evt, t) {
			out = append(out, evt)
		}
	}
	return out
}

func socialCommentsOf(t socialTarget, events []nostr.Event) []nostr.Event {
	out := make([]nostr.Event, 0, len(events))
	for _, evt := range events {
		if evt.Kind == nostr.KindComment && socialMatches(evt, t) {
			out = append(out, evt)
		}
	}
	return out
}

func socialMentionsOf(t socialTarget, events []nostr.Event) []nostr.Event {
	out := make([]nostr.Event, 0, len(events))
	for _, evt := range events {
		if evt.Kind == nostr.KindTextNote && socialMatches(evt, t) {
			out = append(out, evt)
		}
	}
	return out
}

// mentionSummary counts t's live mentions.
func mentionSummary(t socialTarget, mentions, deletions []nostr.Event) ServiceSocialMentionSummary {
	return ServiceSocialMentionSummary{Count: len(socialLive(socialMentionsOf(t, mentions), deletions))}
}

// mentionItems lists t's mentions newest first. Pass live events.
func mentionItems(t socialTarget, mentions []nostr.Event) []ServiceMention {
	mentions = slices.Clone(socialMentionsOf(t, mentions))
	slices.SortFunc(mentions, socialOrder)
	out := []ServiceMention{}
	for _, evt := range mentions {
		text, truncated := socialText(evt.Content, socialMentionRunes)
		out = append(out, ServiceMention{
			ID: evt.ID.Hex(), Author: evt.PubKey.Hex(), Content: text, Truncated: truncated,
			CreatedAt: int64(evt.CreatedAt), Quote: socialMentionQuote(evt, t),
		})
	}
	return out
}

// socialSummary aggregates the live reactions and comments of t. Sync is
// left for the caller, Mentions for socialAggregate; Own is nil without a
// viewer.
func socialSummary(t socialTarget, reactions, comments, deletions []nostr.Event, viewer *nostr.PubKey) ServiceSocialSummary {
	reactions = socialLive(socialReactionsOf(t, reactions), deletions)
	comments = socialLive(socialCommentsOf(t, comments), deletions)
	summary := ServiceSocialSummary{
		Address: t.Address,
		Sync:    map[string]ServiceSocialSync{},
		Emoji:   []ServiceSocialEmoji{},
		ByEvent: []ServiceSocialByEvent{},
	}

	// votes, and their per-version buckets
	buckets := map[string]*ServiceSocialByEvent{}
	bucket := func(id string) *ServiceSocialByEvent {
		b, ok := buckets[id]
		if !ok {
			b = &ServiceSocialByEvent{EventID: id}
			buckets[id] = b
		}
		return b
	}
	votes := latestVotes(reactions)
	for _, evt := range votes {
		b := bucket(socialVersionOf(evt, t))
		if isUpVote(evt.Content) {
			summary.Votes.Up++
			b.Up++
		} else {
			summary.Votes.Down++
			b.Down++
		}
	}
	summary.Votes.Score = summary.Votes.Up - summary.Votes.Down

	// emoji: distinct authors per content, overall and per version
	type emojiGroup struct {
		authors map[nostr.PubKey]bool
		url     string
		urlFrom nostr.Event
	}
	groups := map[string]*emojiGroup{}
	type authorContent struct {
		author  nostr.PubKey
		content string
	}
	perVersion := map[string]map[authorContent]bool{}
	for _, evt := range reactions {
		if isVoteContent(evt.Content) {
			continue
		}
		g, ok := groups[evt.Content]
		if !ok {
			g = &emojiGroup{authors: map[nostr.PubKey]bool{}}
			groups[evt.Content] = g
		}
		g.authors[evt.PubKey] = true
		if u := socialEmojiTagURL(evt, socialShortcode(evt.Content)); u != "" && (g.url == "" || newerEvent(evt, g.urlFrom)) {
			g.url, g.urlFrom = u, evt
		}
		version := socialVersionOf(evt, t)
		seen, ok := perVersion[version]
		if !ok {
			seen = map[authorContent]bool{}
			perVersion[version] = seen
		}
		key := authorContent{evt.PubKey, evt.Content}
		if !seen[key] {
			seen[key] = true
			bucket(version).Emoji++
		}
	}
	for content, g := range groups {
		text, _ := socialText(content, socialReactionRunes)
		summary.Emoji = append(summary.Emoji, ServiceSocialEmoji{
			Content: text, Shortcode: socialShortcode(content), URL: g.url, Count: len(g.authors),
		})
	}
	slices.SortFunc(summary.Emoji, func(a, b ServiceSocialEmoji) int {
		if a.Count != b.Count {
			return cmp.Compare(b.Count, a.Count)
		}
		return cmp.Compare(a.Content, b.Content)
	})
	if len(summary.Emoji) > socialEmojiLimit {
		summary.Emoji = summary.Emoji[:socialEmojiLimit]
	}
	for _, b := range buckets {
		summary.ByEvent = append(summary.ByEvent, *b)
	}
	slices.SortFunc(summary.ByEvent, func(a, b ServiceSocialByEvent) int {
		if wa, wb := a.Up+a.Down+a.Emoji, b.Up+b.Down+b.Emoji; wa != wb {
			return cmp.Compare(wb, wa)
		}
		return cmp.Compare(a.EventID, b.EventID)
	})
	if len(summary.ByEvent) > socialEmojiLimit {
		summary.ByEvent = summary.ByEvent[:socialEmojiLimit]
	}

	// comments
	summary.Comments.Count = len(comments)
	for _, evt := range comments {
		at := int64(evt.CreatedAt)
		if summary.Comments.LatestAt == nil || at > *summary.Comments.LatestAt {
			summary.Comments.LatestAt = &at
		}
	}

	// the viewer's own state
	if viewer != nil {
		summary.Own = socialOwnState(t, reactions, *viewer)
	}
	return summary
}

// socialOwnState is the viewer's latest vote and their emoji reactions,
// newest first. Pass live reactions.
func socialOwnState(t socialTarget, reactions []nostr.Event, viewer nostr.PubKey) *ServiceSocialOwn {
	reactions = socialReactionsOf(t, reactions)
	own := &ServiceSocialOwn{Vote: "none", Reactions: []ServiceSocialOwnReaction{}}
	if evt, ok := latestVotes(reactions)[viewer]; ok {
		own.Vote = "down"
		if isUpVote(evt.Content) {
			own.Vote = "up"
		}
		own.VoteEventID = evt.ID.Hex()
	}
	var mine []nostr.Event
	for _, evt := range reactions {
		if evt.PubKey == viewer && !isVoteContent(evt.Content) {
			mine = append(mine, evt)
		}
	}
	slices.SortFunc(mine, socialOrder)
	for _, evt := range mine {
		if len(own.Reactions) == socialEmojiLimit {
			break
		}
		text, _ := socialText(evt.Content, socialReactionRunes)
		own.Reactions = append(own.Reactions, ServiceSocialOwnReaction{
			ID: evt.ID.Hex(), Content: text, EventID: socialVersionOf(evt, t),
		})
	}
	return own
}

// reactionItems lists t's reactions newest first, all of them when eventID
// is "" and otherwise those on that version. Pass live events.
func reactionItems(t socialTarget, reactions []nostr.Event, eventID string) []ServiceReaction {
	reactions = slices.Clone(socialReactionsOf(t, reactions))
	slices.SortFunc(reactions, socialOrder)
	out := []ServiceReaction{}
	for _, evt := range reactions {
		version := socialVersionOf(evt, t)
		if eventID != "" && version != eventID {
			continue
		}
		text, _ := socialText(evt.Content, socialReactionRunes)
		shortcode := socialShortcode(evt.Content)
		out = append(out, ServiceReaction{
			ID: evt.ID.Hex(), Author: evt.PubKey.Hex(), Content: text,
			Shortcode: shortcode, URL: socialEmojiTagURL(evt, shortcode),
			EventID: version, CreatedAt: int64(evt.CreatedAt),
		})
	}
	return out
}

// socialCommentParent is a comment's parent comment id: its first lowercase
// "e" tag, unless that names a version of t (a top-level comment) or the
// comment itself, or is not an event id.
func socialCommentParent(evt nostr.Event, t socialTarget) string {
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "e" {
			if !hex64.MatchString(tag[1]) || slices.Contains(t.VersionIDs, tag[1]) || tag[1] == evt.ID.Hex() {
				return ""
			}
			return tag[1]
		}
	}
	return ""
}

// commentItems lists the children of parentID newest first. The top level
// (parentID "") also holds orphans, replies whose parent is not held; they
// keep their parent_id. Pass live events.
func commentItems(t socialTarget, comments []nostr.Event, parentID string, viewer *nostr.PubKey) []ServiceComment {
	comments = slices.Clone(socialCommentsOf(t, comments))
	slices.SortFunc(comments, socialOrder)
	held := make(map[string]bool, len(comments))
	for _, evt := range comments {
		held[evt.ID.Hex()] = true
	}
	parents := make(map[string]string, len(comments))
	replies := map[string]int{}
	for _, evt := range comments {
		p := socialCommentParent(evt, t)
		parents[evt.ID.Hex()] = p
		if p != "" {
			replies[p]++
		}
	}
	out := []ServiceComment{}
	for _, evt := range comments {
		id := evt.ID.Hex()
		p := parents[id]
		if parentID == "" {
			if p != "" && held[p] {
				continue
			}
		} else if p != parentID {
			continue
		}
		text, truncated := socialText(evt.Content, socialCommentRunes)
		out = append(out, ServiceComment{
			ID: id, Author: evt.PubKey.Hex(), Content: text, Truncated: truncated,
			CreatedAt: int64(evt.CreatedAt), ParentID: p, ReplyCount: replies[id],
			Own: viewer != nil && evt.PubKey == *viewer,
		})
	}
	return out
}
