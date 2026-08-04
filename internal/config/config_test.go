package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

var configEnvKeys = []string{
	"APP_PORT", "APP_BASE_PATH", "CPA_PUBLIC_URL", "WORK_DIR", "CPA_BASE_URL", "CPA_MANAGEMENT_KEY", "POLL_INTERVAL",
	"USAGE_SYNC_MODE", "REDIS_QUEUE_ADDR", "REDIS_QUEUE_TLS", "REDIS_QUEUE_BATCH_SIZE", "REDIS_QUEUE_IDLE_INTERVAL",
	"DATABASE_URL",
	"REQUEST_TIMEOUT", "LOG_LEVEL", "LOG_FILE_ENABLED", "LOG_DIR", "LOG_RETENTION_DAYS",
	"AUTH_ENABLED", "LOGIN_PASSWORD", "AUTH_SESSION_TTL", "TZ", "TLS_SKIP_VERIFY", "QUOTA_REFRESH_WORKER_LIMIT", "QUOTA_AUTO_REFRESH_ENABLED", "QUOTA_AUTO_REFRESH_INTERVAL",
	"HTTP_READ_HEADER_TIMEOUT", "HTTP_IDLE_TIMEOUT", "SHUTDOWN_TIMEOUT",
	"DB_MAX_OPEN_CONNS", "DB_MAX_IDLE_CONNS", "DB_CONN_MAX_LIFETIME", "DB_CONN_MAX_IDLE_TIME",
}

func TestMain(m *testing.M) {
	previousEnv := make(map[string]string, len(configEnvKeys))
	previousPresent := make(map[string]bool, len(configEnvKeys))
	for _, key := range configEnvKeys {
		previousEnv[key], previousPresent[key] = os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	for _, key := range configEnvKeys {
		if previousPresent[key] {
			if err := os.Setenv(key, previousEnv[key]); err != nil {
				panic(err)
			}
			continue
		}
		if err := os.Unsetenv(key); err != nil {
			panic(err)
		}
	}
	os.Exit(code)
}

func withIsolatedEnvFiles(t *testing.T) {
	t.Helper()
	previousEnv := make(map[string]string, len(configEnvKeys))
	previousPresent := make(map[string]bool, len(configEnvKeys))
	for _, key := range configEnvKeys {
		previousEnv[key], previousPresent[key] = os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
	t.Cleanup(func() {
		for _, key := range configEnvKeys {
			if previousPresent[key] {
				if err := os.Setenv(key, previousEnv[key]); err != nil {
					t.Fatalf("restore %s: %v", key, err)
				}
				continue
			}
			if err := os.Unsetenv(key); err != nil {
				t.Fatalf("unset %s: %v", key, err)
			}
		}
	})
	cwd := t.TempDir()
	exeDir := t.TempDir()
	previousExecutableDir := executableDir
	previousWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		executableDir = previousExecutableDir
		if err := os.Chdir(previousWorkingDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	executableDir = func() (string, error) { return exeDir, nil }
}

func TestLoadFromEnvAppliesDefaults(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	// #395 后 AUTH_ENABLED 默认 true 且无密码会报错；本用例聚焦非 auth 默认值，显式关闭。
	t.Setenv("AUTH_ENABLED", "false")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.AppPort != "8080" {
		t.Fatalf("expected default app port 8080, got %s", cfg.AppPort)
	}
	if cfg.AppBasePath != "" {
		t.Fatalf("expected default app base path to be empty, got %q", cfg.AppBasePath)
	}
	if cfg.CPAPublicURL != "" {
		t.Fatalf("expected default CPA public URL to be empty, got %q", cfg.CPAPublicURL)
	}
	if cfg.WorkDir != filepath.Join(".", "data") {
		t.Fatalf("expected default work dir ./data, got %s", cfg.WorkDir)
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Fatalf("expected default request timeout 30s, got %s", cfg.RequestTimeout)
	}
	if cfg.AuthEnabled {
		t.Fatal("expected auth to be disabled by default")
	}
	if cfg.AuthSessionTTL != 7*24*time.Hour {
		t.Fatalf("expected default auth session ttl 168h, got %s", cfg.AuthSessionTTL)
	}
	if cfg.TLSSkipVerify {
		t.Fatal("expected TLS skip verify to be disabled by default")
	}
	if cfg.RedisQueueTLS {
		t.Fatal("expected redis queue TLS to be disabled by default")
	}
	if cfg.RedisQueueAddr != "" {
		t.Fatalf("expected default redis queue addr to be empty, got %q", cfg.RedisQueueAddr)
	}
	if cfg.RedisQueueBatchSize != 10000 {
		t.Fatalf("expected default redis queue batch size 10000, got %d", cfg.RedisQueueBatchSize)
	}
	if cfg.RedisQueueIdleInterval != time.Second {
		t.Fatalf("expected default redis queue idle interval 1s, got %s", cfg.RedisQueueIdleInterval)
	}
	if cfg.MetadataSyncInterval != MetadataSyncIntervalDefault {
		t.Fatalf("expected default metadata sync interval 30s, got %s", cfg.MetadataSyncInterval)
	}
	if cfg.QuotaRefreshWorkerLimit != 10 {
		t.Fatalf("expected default quota refresh worker limit 10, got %d", cfg.QuotaRefreshWorkerLimit)
	}
	if cfg.QuotaAutoRefreshEnabled {
		t.Fatal("expected quota auto refresh to be disabled by default")
	}
	if cfg.QuotaAutoRefreshInterval != 5*time.Minute {
		t.Fatalf("expected default quota auto refresh interval 5m, got %s", cfg.QuotaAutoRefreshInterval)
	}
	if !cfg.LogFileEnabled {
		t.Fatal("expected log file output to be enabled by default")
	}
	if cfg.LogDir != filepath.Join("data", "logs") {
		t.Fatalf("expected default log dir data/logs, got %s", cfg.LogDir)
	}
	if cfg.LogRetentionDays != 7 {
		t.Fatalf("expected default log retention 7 days, got %d", cfg.LogRetentionDays)
	}
	if cfg.HTTPReadHeaderTimeout != HTTPReadHeaderTimeoutDefault {
		t.Fatalf("expected default http read header timeout %s, got %s", HTTPReadHeaderTimeoutDefault, cfg.HTTPReadHeaderTimeout)
	}
	if cfg.HTTPIdleTimeout != HTTPIdleTimeoutDefault {
		t.Fatalf("expected default http idle timeout %s, got %s", HTTPIdleTimeoutDefault, cfg.HTTPIdleTimeout)
	}
	if cfg.ShutdownTimeout != ShutdownTimeoutDefault {
		t.Fatalf("expected default shutdown timeout %s, got %s", ShutdownTimeoutDefault, cfg.ShutdownTimeout)
	}
	if cfg.DBMaxOpenConns != DBMaxOpenConnsDefault {
		t.Fatalf("expected default db max open conns %d, got %d", DBMaxOpenConnsDefault, cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != DBMaxIdleConnsDefault {
		t.Fatalf("expected default db max idle conns %d, got %d", DBMaxIdleConnsDefault, cfg.DBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime != DBConnMaxLifetimeDefault {
		t.Fatalf("expected default db conn max lifetime %s, got %s", DBConnMaxLifetimeDefault, cfg.DBConnMaxLifetime)
	}
	if cfg.DBConnMaxIdleTime != DBConnMaxIdleTimeDefault {
		t.Fatalf("expected default db conn max idle time %s, got %s", DBConnMaxIdleTimeDefault, cfg.DBConnMaxIdleTime)
	}
}

func TestLoadFromEnvDBPool(t *testing.T) {
	withIsolatedEnvFiles(t)
	env := map[string]string{
		"CPA_BASE_URL":          "https://cpa.example.com",
		"CPA_MANAGEMENT_KEY":    "secret",
		"DB_MAX_OPEN_CONNS":     "40",
		"DB_MAX_IDLE_CONNS":     "20",
		"DB_CONN_MAX_LIFETIME":  "15m",
		"DB_CONN_MAX_IDLE_TIME": "5m",
	}
	for key, value := range env {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("set env %s: %v", key, err)
		}
		t.Cleanup(func() { _ = os.Unsetenv(key) })
	}

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if cfg.DBMaxOpenConns != 40 {
		t.Fatalf("expected db max open conns 40, got %d", cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != 20 {
		t.Fatalf("expected db max idle conns 20, got %d", cfg.DBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime != 15*time.Minute {
		t.Fatalf("expected db conn max lifetime 15m, got %s", cfg.DBConnMaxLifetime)
	}
	if cfg.DBConnMaxIdleTime != 5*time.Minute {
		t.Fatalf("expected db conn max idle time 5m, got %s", cfg.DBConnMaxIdleTime)
	}
}

func TestLoadClampsDBIdleConnsToOpenConns(t *testing.T) {
	withIsolatedEnvFiles(t)
	env := map[string]string{
		"CPA_BASE_URL":       "https://cpa.example.com",
		"CPA_MANAGEMENT_KEY": "secret",
		"DB_MAX_OPEN_CONNS":  "10",
		"DB_MAX_IDLE_CONNS":  "50", // 大于 open，应被夹到 10
	}
	for key, value := range env {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("set env %s: %v", key, err)
		}
		t.Cleanup(func() { _ = os.Unsetenv(key) })
	}

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if cfg.DBMaxIdleConns != 10 {
		t.Fatalf("expected idle conns clamped to open (10), got %d", cfg.DBMaxIdleConns)
	}
}

func TestLoadRejectsNonPositiveDBPool(t *testing.T) {
	withIsolatedEnvFiles(t)
	if err := os.Setenv("CPA_BASE_URL", "https://cpa.example.com"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("CPA_BASE_URL") })
	if err := os.Setenv("CPA_MANAGEMENT_KEY", "secret"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("CPA_MANAGEMENT_KEY") })

	cases := map[string]string{
		"DB_MAX_OPEN_CONNS":     "0",
		"DB_MAX_IDLE_CONNS":     "0",
		"DB_CONN_MAX_LIFETIME":  "0s",
		"DB_CONN_MAX_IDLE_TIME": "0s",
	}
	for key, value := range cases {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("set env %s: %v", key, err)
		}
		if _, err := LoadFromEnv(); err == nil {
			t.Fatalf("expected LoadFromEnv to reject non-positive %s", key)
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset env %s: %v", key, err)
		}
	}
}

func TestLoadFromEnvHTTPAndShutdownTimeouts(t *testing.T) {
	withIsolatedEnvFiles(t)
	env := map[string]string{
		"CPA_BASE_URL":             "https://cpa.example.com",
		"CPA_MANAGEMENT_KEY":       "secret",
		"HTTP_READ_HEADER_TIMEOUT": "12s",
		"HTTP_IDLE_TIMEOUT":        "90s",
		"SHUTDOWN_TIMEOUT":         "15s",
	}
	for key, value := range env {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("set env %s: %v", key, err)
		}
		t.Cleanup(func() { _ = os.Unsetenv(key) })
	}

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if cfg.HTTPReadHeaderTimeout != 12*time.Second {
		t.Fatalf("expected http read header timeout 12s, got %s", cfg.HTTPReadHeaderTimeout)
	}
	if cfg.HTTPIdleTimeout != 90*time.Second {
		t.Fatalf("expected http idle timeout 90s, got %s", cfg.HTTPIdleTimeout)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Fatalf("expected shutdown timeout 15s, got %s", cfg.ShutdownTimeout)
	}
}

func TestLoadRejectsNonPositiveHTTPTimeouts(t *testing.T) {
	withIsolatedEnvFiles(t)
	if err := os.Setenv("CPA_BASE_URL", "https://cpa.example.com"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("CPA_BASE_URL") })
	if err := os.Setenv("CPA_MANAGEMENT_KEY", "secret"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("CPA_MANAGEMENT_KEY") })

	cases := []string{"HTTP_READ_HEADER_TIMEOUT", "HTTP_IDLE_TIMEOUT", "SHUTDOWN_TIMEOUT"}
	for _, key := range cases {
		if err := os.Setenv(key, "0s"); err != nil {
			t.Fatalf("set env %s: %v", key, err)
		}
		if _, err := LoadFromEnv(); err == nil {
			t.Fatalf("expected LoadFromEnv to reject non-positive %s", key)
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset env %s: %v", key, err)
		}
	}
}

func TestLoadReadsSpecifiedEnvFile(t *testing.T) {
	withIsolatedEnvFiles(t)
	envDir := t.TempDir()
	envPath := filepath.Join(envDir, "custom.env")
	if err := os.WriteFile(envPath, []byte("CPA_BASE_URL=https://from-file.example.com\nCPA_MANAGEMENT_KEY=from-file\nAPP_PORT=9091\nWORK_DIR=./custom-data\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	cfg, err := Load(LoadOptions{EnvFile: envPath})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.CPABaseURL != "https://from-file.example.com" || cfg.CPAManagementKey != "from-file" || cfg.AppPort != "9091" || cfg.WorkDir != filepath.Join(envDir, "custom-data") || cfg.LogDir != filepath.Join(envDir, "custom-data", "logs") {
		t.Fatalf("expected config values from specified env file, got %+v", cfg)
	}
}

func TestLoadResolvesRelativeEnvFilePathBase(t *testing.T) {
	withIsolatedEnvFiles(t)
	cwd := t.TempDir()
	previousWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWorkingDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.Mkdir("config", 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join("config", "app.env"), []byte("CPA_BASE_URL=https://relative-env.example.com\nCPA_MANAGEMENT_KEY=relative\nWORK_DIR=./data\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	cfg, err := Load(LoadOptions{EnvFile: filepath.Join("config", "app.env")})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	envFileAbsolutePath, err := filepath.Abs(filepath.Join("config", "app.env"))
	if err != nil {
		t.Fatalf("resolve env file path: %v", err)
	}
	expectedWorkDir := filepath.Join(filepath.Dir(envFileAbsolutePath), "data")
	if cfg.WorkDir != expectedWorkDir || cfg.LogDir != filepath.Join(expectedWorkDir, "logs") {
		t.Fatalf("expected paths under %q, got %+v", expectedWorkDir, cfg)
	}
}

func TestLoadIgnoresLegacyPathOverrides(t *testing.T) {
	withIsolatedEnvFiles(t)
	envDir := t.TempDir()
	envPath := filepath.Join(envDir, "legacy.env")
	content := "CPA_BASE_URL=https://legacy.example.com\nCPA_MANAGEMENT_KEY=legacy\nWORK_DIR=./work\nLOG_DIR=./legacy/logs\n"
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	cfg, err := Load(LoadOptions{EnvFile: envPath})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	expectedWorkDir := filepath.Join(envDir, "work")
	// 与上游一致：legacy LOG_DIR 被解析成相对 WORK_DIR（即 work/legacy/logs），而非相对 env 文件目录。
	// fork 用默认日志目录（workDir/logs），忽略外部 LOG_DIR override 的相对语义。
	if cfg.WorkDir != expectedWorkDir || cfg.LogDir != filepath.Join(expectedWorkDir, "logs") {
		t.Fatalf("expected legacy LOG_DIR override to be ignored, got %+v", cfg)
	}
}

func TestLoadRejectsMissingSpecifiedEnvFile(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing.env")

	_, err := Load(LoadOptions{EnvFile: missingPath})
	if err == nil || !strings.Contains(err.Error(), "stat env file") {
		t.Fatalf("expected missing specified env file error, got %v", err)
	}
}

func TestLoadFallsBackToExecutableDirEnv(t *testing.T) {
	withIsolatedEnvFiles(t)
	exeDir, err := executableDir()
	if err != nil {
		t.Fatalf("get executable dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(exeDir, ".env"), []byte("CPA_BASE_URL=https://from-exe.example.com\nCPA_MANAGEMENT_KEY=from-exe\nWORK_DIR=./data\n"), 0o600); err != nil {
		t.Fatalf("write executable env file: %v", err)
	}

	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.CPABaseURL != "https://from-exe.example.com" || cfg.CPAManagementKey != "from-exe" || cfg.WorkDir != filepath.Join(exeDir, "data") || cfg.LogDir != filepath.Join(exeDir, "data", "logs") {
		t.Fatalf("expected config values from executable dir env, got %+v", cfg)
	}
}

func TestDefaultTimeZoneIsLoadable(t *testing.T) {
	location, err := time.LoadLocation(DefaultTimeZone)
	if err != nil {
		t.Fatalf("expected default timezone %s to be loadable: %v", DefaultTimeZone, err)
	}
	if location.String() != DefaultTimeZone {
		t.Fatalf("expected location %s, got %s", DefaultTimeZone, location)
	}
}

func TestLoadFromEnvAppliesDefaultTimeZone(t *testing.T) {
	previousLocal := time.Local
	t.Cleanup(func() { time.Local = previousLocal })
	t.Setenv("TZ", "")
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")

	_, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if time.Local.String() != "Asia/Shanghai" {
		t.Fatalf("expected default local timezone Asia/Shanghai, got %s", time.Local)
	}
}

func TestLoadFromEnvHonorsExplicitTimeZone(t *testing.T) {
	previousLocal := time.Local
	t.Cleanup(func() { time.Local = previousLocal })
	t.Setenv("TZ", "UTC")
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")

	_, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if time.Local.String() != "UTC" {
		t.Fatalf("expected explicit local timezone UTC, got %s", time.Local)
	}
}

func TestLoadFromEnvHonorsExplicitIANATimeZone(t *testing.T) {
	previousLocal := time.Local
	t.Cleanup(func() { time.Local = previousLocal })
	t.Setenv("TZ", "America/New_York")
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")

	_, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if time.Local.String() != "America/New_York" {
		t.Fatalf("expected explicit local timezone America/New_York, got %s", time.Local)
	}
}

func TestLoadFromEnvRejectsInvalidTimeZone(t *testing.T) {
	previousLocal := time.Local
	t.Cleanup(func() { time.Local = previousLocal })
	t.Setenv("TZ", "Not/AZone")
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")

	_, err := LoadFromEnv()
	if err == nil || !strings.Contains(err.Error(), "TZ is invalid") {
		t.Fatalf("expected invalid TZ error, got %v", err)
	}
}

func TestLoadFromEnvRequiresCriticalValues(t *testing.T) {
	withIsolatedEnvFiles(t)

	t.Run("missing base url", func(t *testing.T) {
		t.Setenv("CPA_MANAGEMENT_KEY", "secret")

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "CPA_BASE_URL is required" {
			t.Fatalf("expected CPA_BASE_URL required error, got %v", err)
		}
	})

	t.Run("missing management key", func(t *testing.T) {
		t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "CPA_MANAGEMENT_KEY is required" {
			t.Fatalf("expected CPA_MANAGEMENT_KEY required error, got %v", err)
		}
	})

	t.Run("missing login password when auth enabled", func(t *testing.T) {
		t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
		t.Setenv("CPA_MANAGEMENT_KEY", "secret")
		t.Setenv("AUTH_ENABLED", "true")

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "LOGIN_PASSWORD is required when AUTH_ENABLED is true" {
			t.Fatalf("expected LOGIN_PASSWORD required error, got %v", err)
		}
	})
}

func TestLoadFromEnvIgnoresRemovedLegacySyncEnvVars(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("USAGE_SYNC_MODE", "invalid")
	t.Setenv("POLL_INTERVAL", "not-a-duration")

	_, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv should ignore removed legacy sync env vars, got error: %v", err)
	}
}

func TestLoadFromEnvUsesRedisQueueAddrOverride(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "https://cpa.example.com")
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("REDIS_QUEUE_ADDR", "redis-stream.example.com:6380")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.RedisQueueAddr != "redis-stream.example.com:6380" {
		t.Fatalf("expected redis queue addr override, got %q", cfg.RedisQueueAddr)
	}
}

func TestLoadFromEnvRejectsNonPositiveRedisQueueBatchSize(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("REDIS_QUEUE_BATCH_SIZE", "0")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "REDIS_QUEUE_BATCH_SIZE must be positive" {
		t.Fatalf("expected REDIS_QUEUE_BATCH_SIZE validation error, got %v", err)
	}
}

func TestLoadFromEnvRejectsOversizedRedisQueueBatchSize(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("REDIS_QUEUE_BATCH_SIZE", strconv.Itoa(cpa.ManagementUsageQueueMaxBatchSize+1))

	_, err := LoadFromEnv()
	expected := fmt.Sprintf("REDIS_QUEUE_BATCH_SIZE must be <= %d", cpa.ManagementUsageQueueMaxBatchSize)
	if err == nil || err.Error() != expected {
		t.Fatalf("expected REDIS_QUEUE_BATCH_SIZE max validation error, got %v", err)
	}
}

func TestLoadFromEnvParsesOverrides(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("WORK_DIR", "/tmp/work")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("APP_BASE_PATH", "/cpa/")
	t.Setenv("CPA_PUBLIC_URL", "https://cpa.public.example.com/")
	t.Setenv("REQUEST_TIMEOUT", "15s")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FILE_ENABLED", "false")
	t.Setenv("LOG_RETENTION_DAYS", "14")
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("LOGIN_PASSWORD", "top-secret")
	t.Setenv("AUTH_SESSION_TTL", "12h")
	t.Setenv("REDIS_QUEUE_IDLE_INTERVAL", "2s")
	t.Setenv("TLS_SKIP_VERIFY", "true")
	t.Setenv("REDIS_QUEUE_TLS", "true")
	t.Setenv("QUOTA_REFRESH_WORKER_LIMIT", "8")
	t.Setenv("QUOTA_AUTO_REFRESH_ENABLED", "true")
	t.Setenv("QUOTA_AUTO_REFRESH_INTERVAL", "2m")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if !cfg.TLSSkipVerify {
		t.Fatal("expected TLS skip verify to be enabled when set to true")
	}
	if !cfg.RedisQueueTLS {
		t.Fatal("expected redis queue TLS to be enabled when set to true")
	}
	if cfg.AppPort != "9090" || cfg.AppBasePath != "/cpa" || cfg.CPAPublicURL != "https://cpa.public.example.com/" || cfg.WorkDir != "/tmp/work" || cfg.RequestTimeout != 15*time.Second || cfg.LogLevel != "debug" || cfg.LogFileEnabled || cfg.LogDir != filepath.Join("/tmp/work", "logs") || cfg.LogRetentionDays != 14 || !cfg.AuthEnabled || cfg.LoginPassword != "top-secret" || cfg.AuthSessionTTL != 12*time.Hour || cfg.RedisQueueIdleInterval != 2*time.Second || cfg.QuotaRefreshWorkerLimit != 8 || !cfg.QuotaAutoRefreshEnabled || cfg.QuotaAutoRefreshInterval != 2*time.Minute {
		t.Fatalf("unexpected config override result: %+v", cfg)
	}
}

func TestLoadFromEnvRejectsNegativeLogRetentionDays(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("LOG_RETENTION_DAYS", "-1")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "LOG_RETENTION_DAYS must be non-negative" {
		t.Fatalf("expected LOG_RETENTION_DAYS validation error, got %v", err)
	}
}

func TestLoadFromEnvRejectsOversizedQuotaRefreshWorkerLimit(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("QUOTA_REFRESH_WORKER_LIMIT", "101")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "QUOTA_REFRESH_WORKER_LIMIT must be <= 100" {
		t.Fatalf("expected QUOTA_REFRESH_WORKER_LIMIT max validation error, got %v", err)
	}
}

func TestLoadFromEnvRejectsTooShortQuotaAutoRefreshInterval(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("QUOTA_AUTO_REFRESH_INTERVAL", "59s")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "QUOTA_AUTO_REFRESH_INTERVAL must be >= 60s" {
		t.Fatalf("expected QUOTA_AUTO_REFRESH_INTERVAL validation error, got %v", err)
	}
}

func TestLoadFromEnvRejectsNonPositiveQuotaRefreshWorkerLimit(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("QUOTA_REFRESH_WORKER_LIMIT", "0")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "QUOTA_REFRESH_WORKER_LIMIT must be positive" {
		t.Fatalf("expected QUOTA_REFRESH_WORKER_LIMIT validation error, got %v", err)
	}
}

func TestLoadFromEnvRejectsNonPositiveRedisQueueIdleInterval(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("REDIS_QUEUE_IDLE_INTERVAL", "0s")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "REDIS_QUEUE_IDLE_INTERVAL must be positive" {
		t.Fatalf("expected REDIS_QUEUE_IDLE_INTERVAL validation error, got %v", err)
	}
}

func TestLoadFromEnvIgnoresRemovedMetadataSyncIntervalOverride(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("REDIS_METADATA_SYNC_INTERVAL", "45s")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if cfg.MetadataSyncInterval != MetadataSyncIntervalDefault {
		t.Fatalf("expected removed env overrides to be ignored, got metadata_interval=%s", cfg.MetadataSyncInterval)
	}
}

func TestLoadFromEnvRejectsInvalidBasePath(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("APP_BASE_PATH", "cpa")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "APP_BASE_PATH is invalid: must start with '/'" {
		t.Fatalf("expected APP_BASE_PATH validation error, got %v", err)
	}
}

func TestLoadFromEnvRejectsNonPositiveAuthSessionTTL(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("AUTH_SESSION_TTL", "0s")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "AUTH_SESSION_TTL must be positive" {
		t.Fatalf("expected AUTH_SESSION_TTL validation error, got %v", err)
	}
}
