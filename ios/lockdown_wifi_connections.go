package ios

import "fmt"

const (
	wirelessLockdownDomain   = "com.apple.mobile.wireless_lockdown"
	enableWifiConnectionsKey = "EnableWifiConnections"
)

// SetWifiConnections enables or disables lockdownd connections over Wi-Fi.
func SetWifiConnections(device DeviceEntry, enabled bool) error {
	lockdownConnection, err := ConnectLockdownWithSession(device)
	if err != nil {
		return err
	}
	defer lockdownConnection.Close()

	return lockdownConnection.SetValueForDomain(enableWifiConnectionsKey, wirelessLockdownDomain, enabled)
}

// GetWifiConnections returns whether lockdownd connections over Wi-Fi are enabled.
func GetWifiConnections(device DeviceEntry) (bool, error) {
	lockdownConnection, err := ConnectLockdownWithSession(device)
	if err != nil {
		return false, err
	}
	defer lockdownConnection.Close()

	value, err := lockdownConnection.GetValueForDomain(enableWifiConnectionsKey, wirelessLockdownDomain)
	if err != nil {
		return false, err
	}
	return wifiConnectionsValue(value)
}

func wifiConnectionsValue(value interface{}) (bool, error) {
	enabled, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf(
			"expected bool when querying %s.%s, received %T:%+v",
			wirelessLockdownDomain,
			enableWifiConnectionsKey,
			value,
			value,
		)
	}
	return enabled, nil
}
