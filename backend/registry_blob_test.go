package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"kwakore/backend/netguard"
)

// ─── test rig ───────────────────────────────────────────────────

// countingBlobServer serves one blob from a loopback httptest server and
// counts every request it gets.
type countingBlobServer struct {
	*httptest.Server
	hits atomic.Int64
}

func newCountingBlobServer(t *testing.T, handler http.HandlerFunc) *countingBlobServer {
	t.Helper()
	s := &countingBlobServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func serveBlob(data []byte) http.HandlerFunc {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+hash {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}
}

func blobHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// setUserBlobServers sets the launcher's own Blossom servers for one test.
func setUserBlobServers(t *testing.T, servers ...string) {
	t.Helper()
	stateMu.Lock()
	previous := state.BlossomServers
	state.BlossomServers = append([]string{}, servers...)
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		state.BlossomServers = previous
		stateMu.Unlock()
	})
}

func setBlobMaxBytes(t *testing.T, n int64) {
	t.Helper()
	previous := blobMaxBytes
	blobMaxBytes = n
	t.Cleanup(func() { blobMaxBytes = previous })
}

// ─── tests ──────────────────────────────────────────────────────

func TestBlobDownloadRefusesPrivateHosts(t *testing.T) {
	// the production blobClient: a manifest server tag or an author's 10063
	// list pointing at loopback gets no request out of the launcher
	setUserBlobServers(t)
	blob := []byte("a blob on the loopback interface")
	srv := newCountingBlobServer(t, serveBlob(blob))

	_, err := downloadBlob(t.Context(), []string{srv.URL}, blobHash(blob))
	if err == nil {
		t.Fatal("a loopback manifest server served a blob")
	}
	if !errors.Is(err, netguard.ErrPrivateAddress) {
		t.Fatalf("refusal is not the private-address error: %v", err)
	}
	if n := srv.hits.Load(); n != 0 {
		t.Fatalf("the loopback server got %d requests", n)
	}

	t.Run("install", func(t *testing.T) {
		// a manifest whose only server is 127.0.0.1 installs nothing, and
		// the same server installs it once the user lists it as their own
		r := newContainmentRig(t)
		setUserBlobServers(t)
		index := []byte("<!doctype html><title>private</title>")
		evt := nappEvent(t, "private-server", r.blob(index))
		n, ok := nappFromEvent(evt)
		if !ok {
			t.Fatal("napp event rejected")
		}
		n.Servers = []string{r.server.URL}

		if err := InstallNapp(n); err == nil {
			t.Fatal("install from a loopback manifest server succeeded")
		}
		if hits := r.hits.Load(); hits != 0 {
			t.Fatalf("the loopback manifest server got %d requests", hits)
		}
		if _, ok := InstalledNapp(n.ID); ok {
			t.Fatal("a failed install was recorded")
		}

		setUserBlobServers(t, r.server.URL)
		if err := InstallNapp(n); err != nil {
			t.Fatalf("install from the user's own server: %v", err)
		}
		if hits := r.hits.Load(); hits == 0 {
			t.Fatal("the user's server was never asked")
		}
		Uninstall(n.ID)
	})
}

func TestBlobDownloadTrustsUserServers(t *testing.T) {
	blob := []byte("a blob on the user's own LAN server")
	srv := newCountingBlobServer(t, serveBlob(blob))
	// the trailing slash and the manifest's own copy of the url are both
	// matched after normalization
	setUserBlobServers(t, srv.URL+"/")

	got, err := downloadBlob(t.Context(), []string{srv.URL}, blobHash(blob))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatalf("got %q", got)
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("requests: %d", n)
	}

	// the built-in defaults are used when none were set, but they are not
	// servers the user configured: they get the public-only check (D-20)
	stateMu.Lock()
	state.BlossomServers = nil
	stateMu.Unlock()
	if got := BlossomServers(); len(got) == 0 {
		t.Fatal("no default servers to fetch from")
	}
	trusted := userBlobServers()
	for _, d := range defaultBlossomServers {
		if trusted[d] {
			t.Errorf("default %s is trusted", d)
		}
	}
	if trusted[srv.URL] {
		t.Error("a server the user did not list is trusted")
	}
	before := srv.hits.Load()
	if _, err := downloadBlob(t.Context(), []string{srv.URL}, blobHash(blob)); !errors.Is(err, netguard.ErrPrivateAddress) {
		t.Fatalf("an unconfigured loopback server: %v, want the private-address error", err)
	}
	if srv.hits.Load() != before {
		t.Fatal("an unconfigured loopback server got a request")
	}
}

// TestTrustedBlobRedirectsStayGuarded: a server the user configured may be
// private, but a redirect from it to a private address the user did not
// configure is refused; one to another configured server is followed
// (WR-05).
func TestTrustedBlobRedirectsStayGuarded(t *testing.T) {
	blob := []byte("a blob behind a trusted redirect")
	hash := blobHash(blob)
	// stands in for the LAN or a cloud metadata address
	private := newCountingBlobServer(t, serveBlob(blob))
	mine := newCountingBlobServer(t, serveBlob(blob))
	redirectTo := func(target *countingBlobServer) *countingBlobServer {
		return newCountingBlobServer(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+r.URL.Path, http.StatusFound)
		})
	}
	toPrivate, toMine := redirectTo(private), redirectTo(mine)
	setUserBlobServers(t, toPrivate.URL, toMine.URL, mine.URL)

	_, err := fetchBlobFrom(t.Context(), trustedBlobClient, toPrivate.URL, hash)
	if !errors.Is(err, netguard.ErrPrivateAddress) {
		t.Fatalf("redirect to an unconfigured private host: %v, want the private-address error", err)
	}
	if n := private.hits.Load(); n != 0 {
		t.Fatalf("the unconfigured private host got %d requests", n)
	}
	if _, err := downloadBlob(t.Context(), []string{toPrivate.URL}, hash); err == nil || private.hits.Load() != 0 {
		t.Fatalf("downloadBlob followed the redirect: %v, %d requests", err, private.hits.Load())
	}

	got, err := fetchBlobFrom(t.Context(), trustedBlobClient, toMine.URL, hash)
	if err != nil || !bytes.Equal(got, blob) {
		t.Fatalf("redirect to another configured server: %q, %v", got, err)
	}
	if mine.hits.Load() != 1 {
		t.Fatalf("the configured target got %d requests", mine.hits.Load())
	}
}

func TestBlobHostKey(t *testing.T) {
	for _, c := range []struct{ host, port, want string }{
		{"Example.COM:443", "", "example.com:443"},
		{"[::1]:8080", "", "[::1]:8080"},
		{"::1", "8080", "[::1]:8080"},
		{"127.0.0.1", "80", "127.0.0.1:80"},
	} {
		if got, ok := blobHostKey(c.host, c.port); !ok || got != c.want {
			t.Errorf("blobHostKey(%q, %q) = %q, %v, want %q", c.host, c.port, got, ok, c.want)
		}
	}
	setUserBlobServers(t, "https://Blossom.Example", "http://192.168.1.5:3000/")
	hosts := userBlobHosts()
	if !hosts["blossom.example:443"] || !hosts["192.168.1.5:3000"] || len(hosts) != 2 {
		t.Fatalf("user blob hosts = %v", hosts)
	}
}

func TestBlobDownloadSizeCap(t *testing.T) {
	setBlobMaxBytes(t, 1024)
	big := bytes.Repeat([]byte("x"), 4096)
	good := bytes.Repeat([]byte("y"), 512)
	hash := blobHash(good)

	// advertises 4096 bytes up front: refused before the body is read
	declared := newCountingBlobServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(big)))
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write(big)
	})
	// streams 4096 bytes with no length: refused after blobMaxBytes+1
	streaming := newCountingBlobServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		for i := 0; i < len(big); i += 256 {
			if _, err := w.Write(big[i : i+256]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	})
	right := newCountingBlobServer(t, serveBlob(good))
	setUserBlobServers(t, declared.URL, streaming.URL, right.URL)

	t.Run("declared", func(t *testing.T) {
		_, err := fetchBlobFrom(t.Context(), trustedBlobClient, declared.URL, hash)
		if err == nil {
			t.Fatal("an over-limit Content-Length was accepted")
		}
	})
	t.Run("streamed", func(t *testing.T) {
		_, err := fetchBlobFrom(t.Context(), trustedBlobClient, streaming.URL, hash)
		if err == nil {
			t.Fatal("an over-limit streamed body was accepted")
		}
	})
	t.Run("next server", func(t *testing.T) {
		got, err := downloadBlob(t.Context(), []string{declared.URL, streaming.URL, right.URL}, hash)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, good) {
			t.Fatal("wrong bytes")
		}
		if declared.hits.Load() == 0 || streaming.hits.Load() == 0 || right.hits.Load() == 0 {
			t.Fatalf("hits: %d %d %d", declared.hits.Load(), streaming.hits.Load(), right.hits.Load())
		}
	})
}

func TestBlobDownloadRedirectLimits(t *testing.T) {
	blob := []byte("a blob behind redirects")
	hash := blobHash(blob)
	final := newCountingBlobServer(t, serveBlob(blob))

	// a chain of n redirects ending at the blob: hop k sends to hop k+1
	chain := func(n int) *countingBlobServer {
		var s *countingBlobServer
		s = newCountingBlobServer(t, func(w http.ResponseWriter, r *http.Request) {
			hop, _ := strconv.Atoi(r.URL.Query().Get("hop"))
			if hop+1 >= n {
				http.Redirect(w, r, final.URL+"/"+hash, http.StatusFound)
				return
			}
			http.Redirect(w, r, s.URL+"/"+hash+"?hop="+strconv.Itoa(hop+1), http.StatusFound)
		})
		return s
	}

	three := chain(3)
	four := chain(4)
	setUserBlobServers(t, three.URL, four.URL, final.URL)

	if got, err := fetchBlobFrom(t.Context(), trustedBlobClient, three.URL, hash); err != nil || !bytes.Equal(got, blob) {
		t.Fatalf("three redirects: %q, %v", got, err)
	}
	before := final.hits.Load()
	if _, err := fetchBlobFrom(t.Context(), trustedBlobClient, four.URL, hash); err == nil {
		t.Fatal("four chained redirects were followed")
	}
	if final.hits.Load() != before {
		t.Fatal("the fourth redirect was followed")
	}

	t.Run("https to http", func(t *testing.T) {
		plain := newCountingBlobServer(t, serveBlob(blob))
		secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
		}))
		t.Cleanup(secure.Close)
		setUserBlobServers(t, secure.URL, plain.URL)

		// the trusted client, with the test server's certificate trusted
		previous := trustedBlobClient
		client := *previous
		transport := previous.Transport.(*http.Transport).Clone()
		transport.TLSClientConfig = secure.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		client.Transport = transport
		trustedBlobClient = &client
		t.Cleanup(func() { trustedBlobClient = previous })

		if _, err := downloadBlob(context.Background(), []string{secure.URL}, hash); err == nil {
			t.Fatal("an https server redirected to http and was followed")
		}
		if n := plain.hits.Load(); n != 0 {
			t.Fatalf("the http target got %d requests", n)
		}
	})
}
