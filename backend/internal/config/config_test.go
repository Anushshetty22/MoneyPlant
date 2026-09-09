package config

import (
	// strings checks that configuration errors identify the invalid setting.
	"strings"
	// testing provides isolated environment-variable cases.
	"testing"
	// time compares parsed duration values without converting units manually.
	"time"
)

// TestLoadParsesLiveMonitorDurations verifies that operators can change live
// persistence and startup deadlines without recompiling the API.
func TestLoadParsesLiveMonitorDurations(t *testing.T) {
	setRequiredDefaults(t)
	t.Setenv("LIVE_MONITOR_PERSIST_INTERVAL", "750ms")
	t.Setenv("LIVE_MONITOR_FINAL_PERSIST_TIMEOUT", "4s")
	t.Setenv("LIVE_MONITOR_RESTORE_TIMEOUT", "8s")

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.LiveMonitorPersistenceInterval != 750*time.Millisecond {
		t.Fatalf("persistence interval = %s, want 750ms", loaded.LiveMonitorPersistenceInterval)
	}
	if loaded.LiveMonitorFinalPersistenceTimeout != 4*time.Second {
		t.Fatalf("final persistence timeout = %s, want 4s", loaded.LiveMonitorFinalPersistenceTimeout)
	}
	if loaded.LiveMonitorRestoreTimeout != 8*time.Second {
		t.Fatalf("restore timeout = %s, want 8s", loaded.LiveMonitorRestoreTimeout)
	}
}

// TestLoadUsesSafeLiveMonitorDefaults keeps the normal local setup explicit.
// These values match the bounded behavior that existed before the settings
// became configurable.
func TestLoadUsesSafeLiveMonitorDefaults(t *testing.T) {
	setRequiredDefaults(t)
	for _, key := range []string{
		"LIVE_MONITOR_PERSIST_INTERVAL",
		"LIVE_MONITOR_FINAL_PERSIST_TIMEOUT",
		"LIVE_MONITOR_RESTORE_TIMEOUT",
	} {
		t.Setenv(key, "")
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.LiveMonitorPersistenceInterval != 5*time.Second {
		t.Fatalf("default persistence interval = %s, want 5s", loaded.LiveMonitorPersistenceInterval)
	}
	if loaded.LiveMonitorFinalPersistenceTimeout != 2*time.Second {
		t.Fatalf("default final persistence timeout = %s, want 2s", loaded.LiveMonitorFinalPersistenceTimeout)
	}
	if loaded.LiveMonitorRestoreTimeout != 3*time.Second {
		t.Fatalf("default restore timeout = %s, want 3s", loaded.LiveMonitorRestoreTimeout)
	}
}

// TestLoadRejectsInvalidLiveMonitorDurations ensures a typo cannot silently
// change timing behavior or start an API with an unusable timeout.
func TestLoadRejectsInvalidLiveMonitorDurations(t *testing.T) {
	settings := []struct {
		name  string
		value string
	}{
		{name: "LIVE_MONITOR_PERSIST_INTERVAL", value: "not-a-duration"},
		{name: "LIVE_MONITOR_FINAL_PERSIST_TIMEOUT", value: "0s"},
		{name: "LIVE_MONITOR_RESTORE_TIMEOUT", value: "-1s"},
	}

	for _, setting := range settings {
		t.Run(setting.name, func(t *testing.T) {
			setRequiredDefaults(t)
			t.Setenv(setting.name, setting.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() error = nil, want invalid %s to fail", setting.name)
			}
			if !strings.Contains(err.Error(), setting.name) {
				t.Fatalf("Load() error = %q, want setting name", err)
			}
		})
	}
}

func TestLoadRejectsInvalidLiveMonitorEndpointAndSymbol(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{name: "endpoint scheme", key: "LIVE_MONITOR_WS_URL", value: "https://example.com"},
		{name: "endpoint host", key: "LIVE_MONITOR_WS_URL", value: "wss://"},
		{name: "symbol characters", key: "LIVE_MONITOR_SYMBOL", value: "BTC-USDT"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			setRequiredDefaults(t)
			t.Setenv(testCase.key, testCase.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() error = nil, want invalid %s to fail", testCase.key)
			}
			if !strings.Contains(err.Error(), testCase.key) {
				t.Fatalf("Load() error = %q, want setting name", err)
			}
		})
	}
}

func setRequiredDefaults(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"API_PORT",
		"POSTGRES_PORT",
		"LIVE_MONITOR_MAX_RETRIES",
		"LIVE_MONITOR_SYMBOL",
		"LIVE_MONITOR_WS_URL",
		"LIVE_MONITOR_PERSIST_INTERVAL",
		"LIVE_MONITOR_FINAL_PERSIST_TIMEOUT",
		"LIVE_MONITOR_RESTORE_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}
