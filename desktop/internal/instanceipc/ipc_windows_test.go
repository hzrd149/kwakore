//go:build windows

package instanceipc

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func dialCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPipeRoundTrip(t *testing.T) {
	data := t.TempDir()
	ln, err := Listen(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		b := make([]byte, 1)
		if _, err := io.ReadFull(c, b); err == nil {
			got <- b[0]
		}
	}()
	c, err := Dial(dialCtx(t), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte{'x'}); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-got:
		if b != 'x' {
			t.Fatalf("server read %q", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server read nothing")
	}
}

func TestPipeDialWithoutListener(t *testing.T) {
	if _, err := Dial(dialCtx(t), t.TempDir()); !errors.Is(err, ErrNoInstance) {
		t.Fatalf("Dial with nobody listening: %v, want ErrNoInstance", err)
	}
}

func TestPipeSecondListenFails(t *testing.T) {
	data := t.TempDir()
	ln, err := Listen(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if ln2, err := Listen(data, nil); err == nil {
		ln2.Close()
		t.Fatal("a second Listen took over the pipe name")
	}
}

func TestPipeDACLIsOwnerOnly(t *testing.T) {
	data := t.TempDir()
	ln, err := Listen(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
		}
	}()
	c, err := Dial(dialCtx(t), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	h := windows.Handle(c.(interface{ Fd() uintptr }).Fd())
	sd, err := windows.GetSecurityInfo(h, windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl.AceCount != 1 {
		t.Fatalf("DACL has %d entries, want exactly 1", dacl.AceCount)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		t.Fatalf("ACE type %d, want access-allowed", ace.Header.AceType)
	}
	aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	ours, err := currentUser()
	if err != nil {
		t.Fatal(err)
	}
	if !aceSID.Equals(ours) {
		t.Fatalf("the only ACE is for %s, not us (%s)", aceSID, ours)
	}
	if desc := securityDescriptor(ours); desc == "" || desc != "D:P(A;;GA;;;"+ours.String()+")" {
		t.Fatalf("security descriptor %q", desc)
	}
}

func TestPipeDialRefusesForeignServer(t *testing.T) {
	data := t.TempDir()
	ln, err := Listen(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	// nobody accepts (the client connects to the waiting pipe instance), so
	// the only reader of the seam is Dial itself
	saved := sidMatches
	sidMatches = func(peer, ours *windows.SID) bool { return false }
	c, err := Dial(dialCtx(t), data)
	sidMatches = saved
	if err == nil {
		c.Close()
		t.Fatal("Dial talked to a pipe server of another user")
	}
	if errors.Is(err, ErrNoInstance) {
		t.Fatalf("a foreign pipe server was reported as no instance: %v", err)
	}
}
