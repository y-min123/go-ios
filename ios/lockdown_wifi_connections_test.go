package ios

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWifiConnectionsSetRequest(t *testing.T) {
	request := newSetValue(enableWifiConnectionsKey, wirelessLockdownDomain, true)

	assert.Equal(t, "SetValue", request.Request)
	assert.Equal(t, "com.apple.mobile.wireless_lockdown", request.Domain)
	assert.Equal(t, "EnableWifiConnections", request.Key)
	assert.Equal(t, true, request.Value)
}

func TestWifiConnectionsValue(t *testing.T) {
	enabled, err := wifiConnectionsValue(true)

	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestWifiConnectionsValueRejectsUnexpectedType(t *testing.T) {
	_, err := wifiConnectionsValue("true")

	assert.EqualError(t, err, "expected bool when querying com.apple.mobile.wireless_lockdown.EnableWifiConnections, received string:true")
}
