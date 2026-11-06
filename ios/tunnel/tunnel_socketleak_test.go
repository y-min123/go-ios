package tunnel

import (
	"context"
	"testing"
	"time"

	"github.com/danielpaulus/go-ios/ios"
)

type stubDeviceLister struct{ list ios.DeviceList }

func (s stubDeviceLister) ListDevices() (ios.DeviceList, error) { return s.list, nil }

func devEntry(udid, connType string) ios.DeviceEntry {
	return ios.DeviceEntry{Properties: ios.DeviceProperties{SerialNumber: udid, ConnectionType: connType}}
}

func leakTestManager(entries ...ios.DeviceEntry) *TunnelManager {
	return &TunnelManager{
		dl:                 stubDeviceLister{list: ios.DeviceList{DeviceList: entries}},
		tunnels:            map[string]Tunnel{},
		failedDevices:      map[string]failedDevice{},
		startTunnelTimeout: time.Second,
	}
}

func TestFailedDeviceBackoff(t *testing.T) {
	cases := []struct {
		failCount int
		want      time.Duration
	}{
		{1, 30 * time.Second},
		{2, 60 * time.Second},
		{3, 120 * time.Second},
		{4, 240 * time.Second},
		{5, 300 * time.Second}, // 480s capped to 5 min
		{10, 300 * time.Second},
	}
	for _, c := range cases {
		if got := failedDeviceBackoff(c.failCount); got != c.want {
			t.Errorf("failedDeviceBackoff(%d) = %v, want %v", c.failCount, got, c.want)
		}
	}
}

func TestShouldSkipDevice(t *testing.T) {
	now := time.Now()
	failed := map[string]failedDevice{
		"recent": {lastAttempt: now.Add(-10 * time.Second), failCount: 1}, // 30s window, 10s elapsed → skip
		"stale":  {lastAttempt: now.Add(-31 * time.Second), failCount: 1}, // 30s window, 31s elapsed → retry
	}
	cases := []struct {
		name, udid, conn string
		want             bool
	}{
		{"fresh network attempted", "net", "Network", false},
		{"fresh usb attempted", "fresh", "USB", false},
		{"recent failure backed off", "recent", "USB", true},
		{"stale failure retried", "stale", "USB", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldSkipDevice(devEntry(c.udid, c.conn), failed, now); got != c.want {
				t.Fatalf("shouldSkipDevice = %v, want %v", got, c.want)
			}
		})
	}
}

// 纯 Wi-Fi 设备也要尝试建隧道（CI 上没有真实设备，尝试必然失败并记入 failedDevices）。
func TestUpdateTunnelsAttemptsNetworkDevices(t *testing.T) {
	tm := leakTestManager(devEntry("net-1", "Network"))
	if err := tm.UpdateTunnels(context.Background()); err != nil {
		t.Fatalf("UpdateTunnels: %v", err)
	}
	got, ok := tm.failedDevices["net-1"]
	if !ok || got.failCount != 1 {
		t.Fatalf("network device should be attempted and recorded with failCount=1, got ok=%v entry=%+v", ok, got)
	}
}

// 已有 Wi-Fi 隧道时插上 USB：旧隧道关闭，改用 USB 重建。
func TestUpdateTunnelsSwitchesToUSB(t *testing.T) {
	tm := leakTestManager(devEntry("dev-1", "Network"), devEntry("dev-1", "USB"))
	closed := false
	tm.tunnels["dev-1"] = Tunnel{Udid: "dev-1", connectionType: "Network", closer: func() error { closed = true; return nil }}
	if err := tm.UpdateTunnels(context.Background()); err != nil {
		t.Fatalf("UpdateTunnels: %v", err)
	}
	if !closed {
		t.Fatal("Wi-Fi tunnel should be closed once USB is available")
	}
	if _, ok := tm.failedDevices["dev-1"]; !ok {
		t.Fatal("USB tunnel should be attempted after closing the Wi-Fi tunnel")
	}
}

// 已有 USB 隧道且 USB 仍在：即使同时列出 Wi-Fi 也保持不动。
func TestUpdateTunnelsKeepsUSBTunnel(t *testing.T) {
	tm := leakTestManager(devEntry("dev-1", "Network"), devEntry("dev-1", "USB"))
	closed := false
	tm.tunnels["dev-1"] = Tunnel{Udid: "dev-1", connectionType: "USB", closer: func() error { closed = true; return nil }}
	if err := tm.UpdateTunnels(context.Background()); err != nil {
		t.Fatalf("UpdateTunnels: %v", err)
	}
	if closed || len(tm.failedDevices) != 0 {
		t.Fatalf("USB tunnel must be kept; closed=%v failedDevices=%v", closed, tm.failedDevices)
	}
}

// A device still inside its backoff window must not be retried — the leak this
// whole change prevents. Pre-seeding a long backoff means UpdateTunnels skips it
// and leaves its entry untouched (no new attempt timestamp).
func TestUpdateTunnelsRespectsBackoff(t *testing.T) {
	tm := leakTestManager(devEntry("usb-1", "USB"))
	seeded := time.Now()
	tm.failedDevices["usb-1"] = failedDevice{lastAttempt: seeded, failCount: 5} // 5 min backoff
	if err := tm.UpdateTunnels(context.Background()); err != nil {
		t.Fatalf("UpdateTunnels: %v", err)
	}
	got, ok := tm.failedDevices["usb-1"]
	if !ok {
		t.Fatal("usb-1 should remain in failedDevices (still backed off)")
	}
	if !got.lastAttempt.Equal(seeded) || got.failCount != 5 {
		t.Fatalf("backed-off device must not be retried; entry changed: %+v", got)
	}
}

// failedDevices entries for devices that are no longer connected get pruned, so
// a reconnect retries immediately instead of waiting out a stale backoff.
func TestUpdateTunnelsPrunesDisconnectedFailedDevices(t *testing.T) {
	tm := leakTestManager(devEntry("net-1", "Network"))
	tm.failedDevices["gone"] = failedDevice{lastAttempt: time.Now(), failCount: 2}
	if err := tm.UpdateTunnels(context.Background()); err != nil {
		t.Fatalf("UpdateTunnels: %v", err)
	}
	if _, ok := tm.failedDevices["gone"]; ok {
		t.Fatal("disconnected device should be pruned from failedDevices")
	}
}

// A USB device whose tunnel start fails (no real backend on CI → GetProductVersion
// errors) must be recorded so the next cycle backs off instead of retrying.
func TestUpdateTunnelsRecordsFailure(t *testing.T) {
	tm := leakTestManager(devEntry("usb-1", "USB"))
	if err := tm.UpdateTunnels(context.Background()); err != nil {
		t.Fatalf("UpdateTunnels: %v", err)
	}
	got, ok := tm.failedDevices["usb-1"]
	if !ok || got.failCount != 1 {
		t.Fatalf("failed USB device should be recorded with failCount=1, got ok=%v entry=%+v", ok, got)
	}
}
