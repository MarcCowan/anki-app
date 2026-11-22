// Package config loads settings from environment variables (spec §3.1).
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	// Embed the zoneinfo database: Windows has none, and Go only reads TZ on Unix.
	_ "time/tzdata"
)

// MinPassphraseRunes is the shortest passphrase the server accepts.
const MinPassphraseRunes = 16

// Config is the server configuration.
type Config struct {
	Addr         string
	DBPath       string
	AllowedHosts []string // lower-cased hostnames, no ports
	BackupDir    string   // defaults to <dir of DBPath>/backups
	Passphrase   string   // "" = passphrase mode off
	Location     *time.Location

	// MCPPublicURL is the Claude connector's public origin
	// (https://host[:port], no trailing slash); "" means the connector is off.
	MCPPublicURL string
	MCPAddr      string // the public listener's address; always loopback
}

// MCPEnabled reports whether the Claude connector is on.
func (c Config) MCPEnabled() bool { return c.MCPPublicURL != "" }

// MCPHost returns the lower-cased hostname of MCPPublicURL, without a port.
func (c Config) MCPHost() string {
	u, err := url.Parse(c.MCPPublicURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// Load reads the configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		Addr:     orDefault(getenv("ADDR"), "127.0.0.1:8080"),
		DBPath:   orDefault(getenv("DB_PATH"), "anki-app.db"),
		Location: time.Local,
	}
	c.BackupDir = orDefault(getenv("BACKUP_DIR"), filepath.Join(filepath.Dir(c.DBPath), "backups"))
	for _, h := range strings.Split(orDefault(getenv("ALLOWED_HOSTS"), "localhost,127.0.0.1"), ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			c.AllowedHosts = append(c.AllowedHosts, h)
		}
	}
	if len(c.AllowedHosts) == 0 {
		return Config{}, errors.New("ALLOWED_HOSTS must name at least one host")
	}
	if p := getenv("PASSPHRASE"); p != "" {
		if utf8.RuneCountInString(p) < MinPassphraseRunes {
			return Config{}, fmt.Errorf("PASSPHRASE must be at least %d characters", MinPassphraseRunes)
		}
		c.Passphrase = p
	}
	if tz := strings.TrimSpace(getenv("TZ")); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return Config{}, fmt.Errorf("TZ: %w", err)
		}
		c.Location = loc
	}
	c.MCPAddr = orDefault(getenv("MCP_ADDR"), "127.0.0.1:8081")
	if !isLoopbackAddr(c.MCPAddr) {
		return Config{}, errors.New("MCP_ADDR must be a loopback host:port")
	}
	if raw := strings.TrimSpace(getenv("MCP_PUBLIC_URL")); raw != "" {
		pub, err := publicURL(raw)
		if err != nil {
			return Config{}, err
		}
		if c.Passphrase == "" {
			return Config{}, errors.New("MCP_PUBLIC_URL needs PASSPHRASE to be set")
		}
		c.MCPPublicURL = pub
	}
	return c, nil
}

// publicURL accepts https://host[:port] (a trailing slash is allowed) and
// returns it lower-cased without the slash.
func publicURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", errors.New("MCP_PUBLIC_URL must be https://host[:port] with no path, query or fragment")
	}
	return "https://" + strings.ToLower(u.Host), nil
}

func isLoopbackAddr(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func orDefault(v, def string) string {
	if v = strings.TrimSpace(v); v == "" {
		return def
	}
	return v
}
