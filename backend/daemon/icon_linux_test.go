//go:build linux

package daemon

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fiatjaf.com/nostr"

	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

func TestRPCIcon(t *testing.T) {
	paths := daemonPaths(t)
	if err := os.MkdirAll(paths.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	key := nostr.MustSecretKeyFromHex(strings.Repeat("0", 63) + "3")
	sha := strings.Repeat("ab", 32)
	withIcon := backend.Napp{D: "icon", Name: "Icon", Author: key.Public(), Kind: backend.KindNapplet, Format: backend.FormatNapplet,
		EventID: strings.Repeat("e", 64), CreatedAt: 10, IconSha: sha, IconMime: "image/webp"}
	withIcon.ID = withIcon.Address()
	plain := backend.Napp{D: "plain", Name: "Plain", Author: key.Public(), Kind: backend.KindNapplet, Format: backend.FormatNapplet,
		EventID: strings.Repeat("f", 64), CreatedAt: 10}
	plain.ID = plain.Address()
	data, err := json.Marshal(backend.AppState{InstalledNapps: map[string]backend.Napp{withIcon.ID: withIcon, plain.ID: plain}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.DataDir, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	var icon bytes.Buffer
	if err := png.Encode(&icon, image.NewNRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(paths.DataDir, "icons"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.DataDir, "icons", sha+".png"), icon.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	_, reader, conn, _ := rpcServiceAt(t, paths)

	for _, params := range []string{`{}`, "", `{"address":"bad"}`, `{"address":5}`, `{"address":"` + withIcon.Address() + `","extra":1}`} {
		_, rpcErr, raw := rpcCall(t, reader, conn, "napplet.icon", params)
		if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams || strings.Contains(raw, paths.DataDir) || strings.Contains(raw, "http") {
			t.Fatalf("%q: %+v %s", params, rpcErr, raw)
		}
	}
	for _, address := range []string{"35129:" + key.Public().Hex() + ":missing", plain.Address()} {
		_, rpcErr, raw := rpcCall(t, reader, conn, "napplet.icon", `{"address":"`+address+`"}`)
		if rpcErr == nil || rpcErr.Code != controlprotocol.NotFound || strings.Contains(raw, paths.DataDir) || strings.Contains(raw, "http") {
			t.Fatalf("%s: %+v %s", address, rpcErr, raw)
		}
	}

	result, rpcErr, raw := rpcCall(t, reader, conn, "napplet.icon", `{"address":"`+withIcon.Address()+`"}`)
	if rpcErr != nil {
		t.Fatalf("icon: %+v", rpcErr)
	}
	if strings.Contains(raw, paths.DataDir) {
		t.Fatalf("the icon result names the data directory: %s", raw)
	}
	var got struct {
		Sha256 string `json:"sha256"`
		Mime   string `json:"mime"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Data   string `json:"data"`
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil || got.Sha256 != sha || got.Mime != "image/png" || got.Width != 4 || got.Height != 3 || !bytes.Equal(decoded, icon.Bytes()) {
		t.Fatalf("icon result: %+v %v", got, err)
	}
	if err := controlprotocol.ValidateNamedParams(result, "sha256", "mime", "width", "height", "data"); err != nil {
		t.Fatalf("unexpected fields: %s", result)
	}
}
