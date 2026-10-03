package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxNotificationTitle = 200
	maxNotificationBody  = 2000
	maxNotificationLabel = 80
)

type notificationAction struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type notificationChannel struct {
	ChannelID       string `json:"channelId"`
	Label           string `json:"label"`
	Description     string `json:"description"`
	DefaultPriority string `json:"defaultPriority"`
}

type notificationSendRequest struct {
	Title    string               `json:"title"`
	Body     string               `json:"body"`
	Icon     string               `json:"icon"`
	Actions  []notificationAction `json:"actions"`
	Channel  string               `json:"channel"`
	Priority string               `json:"priority"`
}

func init() {
	handleNap(map[string]napHandler{
		"notify.send":               napNotifySend,
		"notify.dismiss":            napNotifyDismiss,
		"notify.badge":              napNotifyBadge,
		"notify.channel.register":   napNotifyRegisterChannel,
		"notify.permission.request": napNotifyPermission,
	})
}

// napNotifyPermission answers as notify.permission.result, the type the shim
// settles notify.permission.request on (not the usual <type>.result).
func napNotifyPermission(c *napCall) {
	var req struct {
		Channel string `json:"channel"`
	}
	if c.decode(&req) != nil {
		c.replyAs("notify.permission.result", map[string]any{"granted": false})
		return
	}
	c.async(func(context.Context) {
		detail := "The napplet can show plain-text notifications using your system notification service."
		if req.Channel != "" {
			detail += " It requested the " + cleanNotificationText(req.Channel, 100) + " channel."
		}
		granted, err := c.grant(PermNotify, "show system notifications", detail)
		if err != nil {
			granted = false
		}
		if granted {
			granted = c.requestNotifyPermission()
		}
		c.replyAs("notify.permission.result", map[string]any{"granted": granted})
	})
}

func napNotifySend(c *napCall) {
	var req notificationSendRequest
	if err := c.decode(&req); err != nil {
		c.reply(map[string]any{"error": "invalid notification"})
		return
	}
	if err := validateNotification(&req); err != nil {
		// fixed strings, never the error's own text (D-07)
		if errors.Is(err, errNotifyIcon) {
			c.reply(map[string]any{"error": "unsupported icon"})
		} else {
			c.reply(map[string]any{"error": "invalid notification"})
		}
		return
	}
	// a check, never a prompt: notify.permission.request is where the
	// napplet asks (the route's denial code is "permission denied")
	if !c.hasGrant(PermNotify) {
		c.failWith(napErrDenied)
		return
	}

	s := c.ci.nap
	s.mu.Lock()
	if req.Channel != "" {
		if _, ok := s.notifyChannels[req.Channel]; !ok {
			s.mu.Unlock()
			c.reply(map[string]any{"error": "invalid channel"})
			return
		}
	}
	// the window's notify buckets (nap_limits.go: 20 a minute, 3 of them
	// urgent), which a session restart does not refill (D-14). An urgent
	// one is checked against its own bucket first, so a refused urgent
	// notification costs no normal token. Over either limit answers
	// NAP-NOTIFY's own "rate limited".
	if (req.Priority == "urgent" && !s.limits.allow(limitNotifyUrgent, 1)) || !s.limits.allow(limitNotify, 1) {
		s.mu.Unlock()
		c.failWith(napErrRateLimited)
		return
	}
	s.notifySeq++
	id := fmt.Sprintf("%s-%d", c.ci.instance, s.notifySeq)
	s.mu.Unlock()

	actions := make([]NotificationAction, len(req.Actions))
	for i, action := range req.Actions {
		actions[i] = NotificationAction{ID: action.ID, Label: action.Label}
	}
	c.async(func(context.Context) {
		handle, err := c.notify(NotificationRequest{
			ID: id, NappID: c.ci.napp.ID, NappName: c.ci.napp.Label(),
			Title: req.Title, Body: req.Body, Icon: req.Icon, Channel: req.Channel,
			Priority: req.Priority, Actions: actions,
		})
		if errors.Is(err, errSinkRefused) {
			// already answered
			return
		}
		if err != nil || handle == nil {
			if err == nil {
				err = errors.New("notification service returned no handle")
			}
			log.Warn().Err(err).Str("napplet", c.ci.napp.ID).Msg("system notification failed")
			c.reply(map[string]any{"error": "notification unavailable"})
			return
		}
		s.mu.Lock()
		if s.gen != c.gen {
			s.mu.Unlock()
			if handle != nil {
				_ = handle.Dismiss()
			}
			return
		}
		s.notifications[id] = handle
		s.mu.Unlock()
		c.reply(map[string]any{"notificationId": id})
	})
}

func napNotifyDismiss(c *napCall) {
	var req struct {
		NotificationID string `json:"notificationId"`
	}
	if c.decode(&req) != nil || req.NotificationID == "" {
		return
	}
	s := c.ci.nap
	s.mu.Lock()
	handle, ok := s.notifications[req.NotificationID]
	if ok {
		delete(s.notifications, req.NotificationID)
	}
	s.mu.Unlock()
	if ok && handle != nil {
		_ = handle.Dismiss()
	}
}

func napNotifyBadge(c *napCall) {
	var req struct {
		Count int64 `json:"count"`
	}
	if c.decode(&req) != nil || req.Count < 0 {
		return
	}
	c.ci.nap.mu.Lock()
	c.ci.nap.notifyBadge = uint(req.Count)
	c.ci.nap.mu.Unlock()
}

func napNotifyRegisterChannel(c *napCall) {
	var channel notificationChannel
	if c.decode(&channel) != nil || channel.ChannelID == "" || channel.Label == "" ||
		!shortPlain(channel.ChannelID, 100) || !shortPlain(channel.Label, maxNotificationLabel) ||
		(channel.DefaultPriority != "" && !validNotificationPriority(channel.DefaultPriority)) {
		return
	}
	channel.Description = cleanNotificationText(channel.Description, 500)
	c.ci.nap.mu.Lock()
	c.ci.nap.notifyChannels[channel.ChannelID] = channel
	c.ci.nap.mu.Unlock()
}

// validateNotification's two refusals; each answers its own fixed string
var (
	errNotifyInvalid = errors.New("invalid notification")
	errNotifyIcon    = errors.New("unsupported icon")
)

func validateNotification(req *notificationSendRequest) error {
	req.Title = cleanNotificationText(req.Title, maxNotificationTitle)
	req.Body = cleanNotificationText(req.Body, maxNotificationBody)
	if req.Title == "" {
		return errNotifyInvalid
	}
	if req.Icon != "" {
		return errNotifyIcon
	}
	if req.Priority == "" {
		req.Priority = "normal"
	}
	if !validNotificationPriority(req.Priority) || len(req.Actions) > 3 {
		return errNotifyInvalid
	}
	seen := make(map[string]bool, len(req.Actions))
	for i := range req.Actions {
		req.Actions[i].ID = strings.TrimSpace(req.Actions[i].ID)
		req.Actions[i].Label = cleanNotificationText(req.Actions[i].Label, maxNotificationLabel)
		if req.Actions[i].ID == "" || req.Actions[i].Label == "" ||
			!shortPlain(req.Actions[i].ID, 100) || seen[req.Actions[i].ID] {
			return errNotifyInvalid
		}
		seen[req.Actions[i].ID] = true
	}
	return nil
}

func validNotificationPriority(priority string) bool {
	switch priority {
	case "low", "normal", "high", "urgent":
		return true
	}
	return false
}

func shortPlain(s string, max int) bool {
	return s == cleanNotificationText(s, max) && s != ""
}

func cleanNotificationText(s string, max int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !utf8.ValidRune(r) || r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s))
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:max]))
}
