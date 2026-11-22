package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr = %q", c.Addr)
	}
	if c.DBPath != "anki-app.db" {
		t.Errorf("DBPath = %q", c.DBPath)
	}
	if want := []string{"localhost", "127.0.0.1"}; !reflect.DeepEqual(c.AllowedHosts, want) {
		t.Errorf("AllowedHosts = %v", c.AllowedHosts)
	}
	if c.Passphrase != "" {
		t.Errorf("Passphrase = %q", c.Passphrase)
	}
	if c.Location != time.Local {
		t.Errorf("Location = %v, want time.Local", c.Location)
	}
}

func TestLoadBackupDirDefault(t *testing.T) {
	c, err := Load(env(map[string]string{"DB_PATH": "/x/y/anki-app.db"}))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/x/y", "backups"); c.BackupDir != want {
		t.Errorf("BackupDir = %q, want %q", c.BackupDir, want)
	}
	c, err = Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(".", "backups"); c.BackupDir != want {
		t.Errorf("BackupDir = %q, want %q", c.BackupDir, want)
	}
}

func TestLoadBackupDirOverride(t *testing.T) {
	c, err := Load(env(map[string]string{"BACKUP_DIR": " /b "}))
	if err != nil {
		t.Fatal(err)
	}
	if c.BackupDir != "/b" {
		t.Errorf("BackupDir = %q", c.BackupDir)
	}
}

func TestLoadAllowedHosts(t *testing.T) {
	c, err := Load(env(map[string]string{"ALLOWED_HOSTS": " Laptop.tail1.ts.net , localhost "}))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"laptop.tail1.ts.net", "localhost"}; !reflect.DeepEqual(c.AllowedHosts, want) {
		t.Errorf("AllowedHosts = %v", c.AllowedHosts)
	}
}

func TestLoadShortPassphrase(t *testing.T) {
	_, err := Load(env(map[string]string{"PASSPHRASE": strings.Repeat("x", 15)}))
	if err == nil || !strings.Contains(err.Error(), "PASSPHRASE") {
		t.Fatalf("15 chars: err = %v", err)
	}
	c, err := Load(env(map[string]string{"PASSPHRASE": strings.Repeat("x", 16)}))
	if err != nil || c.Passphrase != strings.Repeat("x", 16) {
		t.Fatalf("16 chars: %v", err)
	}
}

func TestLoadTZ(t *testing.T) {
	c, err := Load(env(map[string]string{"TZ": "Europe/London"}))
	if err != nil || c.Location.String() != "Europe/London" {
		t.Fatalf("Europe/London: %v %v", c.Location, err)
	}
	if _, err := Load(env(map[string]string{"TZ": "Nope/Nowhere"})); err == nil {
		t.Fatal("Nope/Nowhere: want error")
	}
}

const testPass = "a-long-enough-passphrase"

func TestLoadMCPOffByDefault(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.MCPEnabled() {
		t.Error("MCPEnabled() = true with MCP_PUBLIC_URL unset")
	}
	if c.MCPAddr != "127.0.0.1:8081" {
		t.Errorf("MCPAddr = %q", c.MCPAddr)
	}
}

func TestLoadMCPPublicURLNormalised(t *testing.T) {
	c, err := Load(env(map[string]string{"MCP_PUBLIC_URL": "https://Anki.x.ts.net:8443/", "PASSPHRASE": testPass}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.MCPEnabled() {
		t.Fatal("MCPEnabled() = false")
	}
	if c.MCPPublicURL != "https://anki.x.ts.net:8443" {
		t.Errorf("MCPPublicURL = %q", c.MCPPublicURL)
	}
	if c.MCPHost() != "anki.x.ts.net" {
		t.Errorf("MCPHost() = %q", c.MCPHost())
	}
}

func TestLoadMCPRejects(t *testing.T) {
	cases := map[string]map[string]string{
		"no passphrase":   {"MCP_PUBLIC_URL": "https://anki.x.ts.net:8443"},
		"http":            {"MCP_PUBLIC_URL": "http://anki.x.ts.net:8443", "PASSPHRASE": testPass},
		"path":            {"MCP_PUBLIC_URL": "https://anki.x.ts.net:8443/x", "PASSPHRASE": testPass},
		"query":           {"MCP_PUBLIC_URL": "https://anki.x.ts.net:8443?a=1", "PASSPHRASE": testPass},
		"fragment":        {"MCP_PUBLIC_URL": "https://anki.x.ts.net:8443#f", "PASSPHRASE": testPass},
		"no host":         {"MCP_PUBLIC_URL": "https://", "PASSPHRASE": testPass},
		"addr any":        {"MCP_ADDR": "0.0.0.0:8081"},
		"addr hostname":   {"MCP_ADDR": "anki:8081"},
		"addr no port":    {"MCP_ADDR": "127.0.0.1"},
		"userinfo in url": {"MCP_PUBLIC_URL": "https://u:p@anki.x.ts.net", "PASSPHRASE": testPass},
	}
	for name, e := range cases {
		if _, err := Load(env(e)); err == nil {
			t.Errorf("%s: Load succeeded, want error", name)
		}
	}
}

func TestLoadMCPAddrIPv6Loopback(t *testing.T) {
	c, err := Load(env(map[string]string{"MCP_ADDR": "[::1]:9000"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.MCPAddr != "[::1]:9000" {
		t.Errorf("MCPAddr = %q", c.MCPAddr)
	}
}
