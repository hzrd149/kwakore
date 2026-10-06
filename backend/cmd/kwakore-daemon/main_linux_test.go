//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateMissingAndInvalidConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	if err := run([]string{"validate"}); err != nil { t.Fatal(err) }
	path := filepath.Join(root,"config","kwakore","config.json")
	if err := os.MkdirAll(filepath.Dir(path),0700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(path, []byte(`{"private_key":"bad"}`),0600); err != nil { t.Fatal(err) }
	if err := run([]string{"validate"}); err == nil { t.Fatal("secret field accepted") }
}
