package house_arrest

import (
	"bytes"
	"fmt"

	"github.com/danielpaulus/go-ios/ios/afc"
	"github.com/pkg/errors"
	"howett.net/plist"

	"github.com/danielpaulus/go-ios/ios"
)

const serviceName = "com.apple.mobile.house_arrest"

const (
	commandVendContainer = "VendContainer"
	commandVendDocuments = "VendDocuments"
)

// New vends the app's whole data container. The device only permits this for apps
// signed with get-task-allow (development builds); distribution-signed apps are
// rejected with InstallationLookupFailed. Use NewDocuments for those.
func New(device ios.DeviceEntry, bundleID string) (*afc.Client, error) {
	return newWithCommand(device, bundleID, commandVendContainer)
}

// NewDocuments vends only the app's Documents directory, which the device permits
// regardless of signing as long as the app sets UIFileSharingEnabled in its Info.plist.
// The AFC root stays the container root, so paths keep the "Documents/" prefix.
func NewDocuments(device ios.DeviceEntry, bundleID string) (*afc.Client, error) {
	return newWithCommand(device, bundleID, commandVendDocuments)
}

func newWithCommand(device ios.DeviceEntry, bundleID string, command string) (*afc.Client, error) {
	deviceConn, err := ios.ConnectToService(device, serviceName)
	if err != nil {
		return nil, err
	}
	err = vend(deviceConn, bundleID, command)
	if err != nil {
		return nil, err
	}
	return afc.NewFromConn(deviceConn), nil
}

func vend(deviceConn ios.DeviceConnectionInterface, bundleID string, command string) error {
	plistCodec := ios.NewPlistCodec()
	vendContainer := map[string]interface{}{"Command": command, "Identifier": bundleID}
	msg, err := plistCodec.Encode(vendContainer)
	if err != nil {
		return fmt.Errorf("vendContainer Encoding cannot fail unless the encoder is broken: %v", err)
	}
	err = deviceConn.Send(msg)
	if err != nil {
		return err
	}
	reader := deviceConn.Reader()
	response, err := plistCodec.Decode(reader)
	if err != nil {
		return err
	}
	return checkResponse(response)
}

func checkResponse(vendContainerResponseBytes []byte) error {
	response, err := plistFromBytes(vendContainerResponseBytes)
	if err != nil {
		return err
	}
	if "Complete" == response.Status {
		return nil
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return errors.New("unknown error during vendcontainer")
}

func plistFromBytes(plistBytes []byte) (vendContainerResponse, error) {
	var vendResponse vendContainerResponse
	decoder := plist.NewDecoder(bytes.NewReader(plistBytes))

	err := decoder.Decode(&vendResponse)
	if err != nil {
		return vendResponse, err
	}
	return vendResponse, nil
}

type vendContainerResponse struct {
	Status string
	Error  string
}
