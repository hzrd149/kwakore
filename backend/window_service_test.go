package backend

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceWindowStopConfirmsExactInstance(t *testing.T) {
	setupNapTest(t)
	selected, _ := openNapplet(t, "selected")
	other, _ := openNapplet(t, "other")
	selected.instance = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	other.instance = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := ServiceStop(ctx, selected.ID()); !errors.Is(err, ErrServiceTimeout) {
		t.Fatalf("stop without WindowClosed: %v", err)
	}
	if other.isGone() {
		t.Fatal("stopping selected closed another window")
	}
	go func() { time.Sleep(20 * time.Millisecond); WindowClosed(selected.ID()) }()
	result, err := ServiceStop(context.Background(), selected.ID())
	if err != nil || !result.Closed || result.WindowID != selected.ID() {
		t.Fatalf("confirmed stop: %+v %v", result, err)
	}
	if other.isGone() {
		t.Fatal("stopping selected closed another window")
	}
	if _, err := ServiceStop(context.Background(), selected.ID()); !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("second stop: %v", err)
	}
}
