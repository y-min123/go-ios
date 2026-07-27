package main

import "testing"

func TestUsedDiskBytes(t *testing.T) {
	if got := usedDiskBytes(100, 40); got != 60 {
		t.Fatalf("usedDiskBytes(100, 40) = %d, want 60", got)
	}
}

func TestDiskspaceByteCount(t *testing.T) {
	if got := diskspaceByteCount(1500); got != "1.5kB" {
		t.Fatalf("diskspaceByteCount(1500) = %q, want %q", got, "1.5kB")
	}
}

func TestWifiConnectionsState(t *testing.T) {
	testCases := map[string]struct {
		state     string
		shouldSet bool
		enabled   bool
	}{
		"get": {
			state: "",
		},
		"enable": {
			state:     "on",
			shouldSet: true,
			enabled:   true,
		},
		"disable": {
			state:     "off",
			shouldSet: true,
			enabled:   false,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			shouldSet, enabled, err := wifiConnectionsState(testCase.state)
			if err != nil {
				t.Fatalf("wifiConnectionsState(%q) returned unexpected error: %v", testCase.state, err)
			}
			if shouldSet != testCase.shouldSet {
				t.Errorf("wifiConnectionsState(%q) shouldSet = %v, want %v", testCase.state, shouldSet, testCase.shouldSet)
			}
			if enabled != testCase.enabled {
				t.Errorf("wifiConnectionsState(%q) enabled = %v, want %v", testCase.state, enabled, testCase.enabled)
			}
		})
	}
}

func TestWifiConnectionsStateRejectsInvalidValue(t *testing.T) {
	_, _, err := wifiConnectionsState("enabled")
	want := `invalid --state value "enabled": expected on or off`
	if err == nil || err.Error() != want {
		t.Fatalf("wifiConnectionsState(%q) error = %v, want %q", "enabled", err, want)
	}
}
