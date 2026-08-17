package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr            string
	DatabasePath        string
	BootstrapAdminEmail string
	BootstrapAdminPass  string
	HeartbeatTimeout    time.Duration
	AccessTokenTTL      time.Duration
	RefreshTokenTTL     time.Duration
	DeviceSessionTTL    time.Duration
	CORSOrigin          string
	StaticDir           string
	TLSCertFile         string
	TLSKeyFile          string
	TLSPassthroughAddr  string
	TrustProxy          bool
	AllowInsecureBind   bool
	PublicBaseURL       string
	ForceRelay          bool
	STUNURLs            []string
	DirectTimeout       time.Duration
	StorageMaxTotal     int64
	StorageMaxFile      int64
	StorageMaxFiles     int64
	SecretsKey          string
	SecretsKeyFile      string
	LogRetentionDays    int
}

func Load() Config {
	stun := getenv("KNOT_STUN_URLS", "stun:stun.l.google.com:19302")
	var stunList []string
	for _, p := range strings.Split(stun, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			stunList = append(stunList, p)
		}
	}
	return Config{
		HTTPAddr:            getenv("KNOT_HTTP_ADDR", "127.0.0.1:8787"),
		DatabasePath:        getenv("KNOT_DB_PATH", "./data/knot.db"),
		BootstrapAdminEmail: os.Getenv("KNOT_BOOTSTRAP_ADMIN"),
		BootstrapAdminPass:  os.Getenv("KNOT_BOOTSTRAP_PASSWORD"),
		HeartbeatTimeout:    durationEnv("KNOT_HEARTBEAT_TIMEOUT", 45*time.Second),
		AccessTokenTTL:      durationEnv("KNOT_ACCESS_TOKEN_TTL", time.Hour),
		RefreshTokenTTL:     durationEnv("KNOT_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		DeviceSessionTTL:    durationEnv("KNOT_DEVICE_SESSION_TTL", 24*time.Hour),
		CORSOrigin:          corsOrigin(),
		StaticDir:           getenv("KNOT_STATIC_DIR", ""),
		TLSCertFile:         os.Getenv("KNOT_TLS_CERT"),
		TLSKeyFile:          os.Getenv("KNOT_TLS_KEY"),
		TLSPassthroughAddr:  os.Getenv("KNOT_TLS_PASSTHROUGH_ADDR"),
		TrustProxy:          boolEnv("KNOT_TRUST_PROXY", false),
		AllowInsecureBind:   boolEnv("KNOT_ALLOW_INSECURE_BIND", false),
		PublicBaseURL:       os.Getenv("KNOT_PUBLIC_BASE_URL"),
		ForceRelay:          boolEnv("KNOT_FORCE_RELAY", false),
		STUNURLs:            stunList,
		DirectTimeout:       durationEnv("KNOT_DIRECT_TIMEOUT", 3*time.Second),
		StorageMaxTotal:     int64Env("KNOT_STORAGE_MAX_TOTAL_BYTES", 0),
		StorageMaxFile:      int64Env("KNOT_STORAGE_MAX_FILE_BYTES", 0),
		StorageMaxFiles:     int64Env("KNOT_STORAGE_MAX_FILES", 0),
		SecretsKey:          os.Getenv("KNOT_SECRETS_KEY"),
		SecretsKeyFile:      os.Getenv("KNOT_SECRETS_KEY_FILE"),
		LogRetentionDays:    intEnv("KNOT_LOG_RETENTION_DAYS", 30),
	}
}

func (c Config) TLSEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}

// ValidateBind enforces: non-loopback bind requires TLS or AllowInsecureBind.
func (c Config) ValidateBind() error {
	host, _, err := net.SplitHostPort(normalizeAddr(c.HTTPAddr))
	if err != nil {
		return fmt.Errorf("invalid KNOT_HTTP_ADDR %q: %w", c.HTTPAddr, err)
	}
	if isLoopbackHost(host) {
		return nil
	}
	if c.TLSEnabled() {
		return nil
	}
	if c.AllowInsecureBind {
		return nil
	}
	return fmt.Errorf("insecure_config: non-loopback bind %q requires TLS (KNOT_TLS_CERT/KNOT_TLS_KEY) or KNOT_ALLOW_INSECURE_BIND=1", c.HTTPAddr)
}

// ValidateBootstrap checks bootstrap credentials when DB has no users.
// Weak password "admin" is forbidden unless bind is loopback.
func (c Config) ValidateBootstrap(userCount int) error {
	if userCount > 0 {
		return nil
	}
	if c.BootstrapAdminEmail == "" || c.BootstrapAdminPass == "" {
		return fmt.Errorf("validation_error: empty database requires KNOT_BOOTSTRAP_ADMIN and KNOT_BOOTSTRAP_PASSWORD")
	}
	host, _, _ := net.SplitHostPort(normalizeAddr(c.HTTPAddr))
	if c.BootstrapAdminPass == "admin" && !isLoopbackHost(host) {
		return fmt.Errorf("insecure_config: bootstrap password 'admin' is not allowed on non-loopback bind")
	}
	return nil
}

func normalizeAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "0.0.0.0" + addr
	}
	return addr
}

func isLoopbackHost(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func corsOrigin() string {
	if v := os.Getenv("KNOT_CORS_ORIGIN"); v != "" {
		return v
	}
	if v := os.Getenv("KNOT_PUBLIC_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:5173"
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func boolEnv(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func durationEnv(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	if sec, err := strconv.Atoi(v); err == nil {
		return time.Duration(sec) * time.Second
	}
	return def
}

func intEnv(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}

func int64Env(k string, def int64) int64 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}
