package imagemounter

import (
	"crypto/sha512"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/Masterminds/semver"
	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/golog"
)

// PersonalizedDeveloperDiskImageMounter allows mounting personalized developer disk images
// that are used starting with iOS 17.
// For personalized developer disk images a nonce gets queried from the device and needs to
// be signed by Apple to be able to mount the developer disk
type PersonalizedDeveloperDiskImageMounter struct {
	deviceConn ios.DeviceConnectionInterface
	plistRw    ios.PlistCodecReadWriter
	version    *semver.Version
	tss        tssClient
	ecid       uint64
	entry      ios.DeviceEntry
}

// NewPersonalizedDeveloperDiskImageMounter creates a PersonalizedDeveloperDiskImageMounter for the device entry
func NewPersonalizedDeveloperDiskImageMounter(entry ios.DeviceEntry, version *semver.Version) (PersonalizedDeveloperDiskImageMounter, error) {
	values, err := ios.GetValuesPlist(entry)
	if err != nil {
		return PersonalizedDeveloperDiskImageMounter{}, fmt.Errorf("NewPersonalizedDeveloperDiskImageMounter: could not read lockdown values: %w", err)
	}
	var ecid uint64
	if e, ok := values["UniqueChipID"].(uint64); ok {
		ecid = e
	} else {
		return PersonalizedDeveloperDiskImageMounter{}, fmt.Errorf("could not get ECID from device")
	}
	deviceConn, err := ios.ConnectToService(entry, serviceName)
	if err != nil {
		return PersonalizedDeveloperDiskImageMounter{}, err
	}
	return PersonalizedDeveloperDiskImageMounter{
		deviceConn: deviceConn,
		plistRw:    ios.NewPlistCodecReadWriter(deviceConn.Reader(), deviceConn.Writer()),
		version:    version,
		tss:        newTssClient(),
		ecid:       ecid,
		entry:      entry,
	}, nil
}

// Close closes the connection to the image mounter service
func (p PersonalizedDeveloperDiskImageMounter) Close() error {
	return p.deviceConn.Close()
}

// ListImages provides a list of signatures of the mounted personalized developer disk images
func (p PersonalizedDeveloperDiskImageMounter) ListImages() ([][]byte, error) {
	return listImages(p.plistRw, "Personalized", p.version)
}

// MountImage mounts the personalized developer disk image present at imagePath.
// imagePath needs to point to the 'Restore' directory of the personalized developer disk image.
//
// MountImage first tries to reuse an existing device-side manifest via QueryPersonalizationManifest
// to avoid unnecessary Apple TSS requests on re-mounts. If no manifest exists, or if mounting with
// that manifest fails, it falls back to querying a nonce and getting a new signature from Apple's
// TSS server.
func (p PersonalizedDeveloperDiskImageMounter) MountImage(imagePath string) error {
	udid := p.entry.Properties.SerialNumber

	manifest, err := loadBuildManifest(path.Join(imagePath, "BuildManifest.plist"))
	if err != nil {
		return fmt.Errorf("MountImage: failed to load build manifest: %w", err)
	}

	identifiers, err := p.queryIdentifiers()
	if err != nil {
		return fmt.Errorf("MountImage: failed to query personalization identifiers: %w", err)
	}

	identity, err := manifest.findIdentity(identifiers)
	if err != nil {
		return fmt.Errorf("MountImage: could not find identity for identifiers %+v: %w", identifiers, err)
	}

	// 键名或结构变化时要给出能直接定位的报错，否则空路径会一路带到 os.Open / os.ReadFile，
	// 报出来的是"打开目录失败"这种看不出根因的错。
	if identity.dmgPath() == "" {
		return fmt.Errorf("MountImage: build manifest has no 'PersonalizedDMG' entry for ApBoardId 0x%x and ApChipId 0x%x", identifiers.BoardId, identifiers.ChipID)
	}
	if identity.trustCachePath() == "" {
		return fmt.Errorf("MountImage: build manifest has no 'LoadableTrustCache' entry for ApBoardId 0x%x and ApChipId 0x%x", identifiers.BoardId, identifiers.ChipID)
	}
	dmgPath := path.Join(imagePath, identity.dmgPath())
	trustCache, err := os.ReadFile(path.Join(imagePath, identity.trustCachePath()))
	if err != nil {
		return fmt.Errorf("MountImage: could not load trust-cache. %w", err)
	}

	// 重连出来的连接外层的 defer 认不到（那边只持有最初那一条），这里自己关掉。
	// closeAndReconnect 会关掉上一条，所以只需要记住最新的一条。
	var reconnectedConn io.Closer
	defer func() {
		if reconnectedConn != nil {
			_ = reconnectedConn.Close()
		}
	}()

	signature, err := p.queryPersonalizationManifest(dmgPath)
	usedDeviceManifest := err == nil
	if usedDeviceManifest {
		golog.Info("reusing existing device-side manifest, skipping Apple TSS", "module", logModule, "udid", udid, "imagePath", imagePath)
	} else {
		golog.Info("no existing device-side manifest, requesting new signature from Apple TSS", "module", logModule, "udid", udid, "imagePath", imagePath)
		p, signature, err = p.signWithTss(identity, identifiers)
		reconnectedConn = p.deviceConn
		if err != nil {
			return err
		}
	}

	mountErr := p.uploadAndMount(signature, dmgPath, trustCache)
	// 已经挂着镜像不是签名问题，重新请签也一样被拒，直接把错误交给上层判定
	if mountErr == nil || !usedDeviceManifest || errors.Is(mountErr, ErrAlreadyMounted) {
		return mountErr
	}

	// 设备端 manifest 可能已经失效（例如设备重启后 nonce 变了）。不回退的话每次连接都会
	// 拿到同一份坏签名，重下镜像也救不回来，表现是永久失败。
	golog.Warn("mounting with the device-side manifest failed, retrying with a fresh Apple TSS signature", "module", logModule, "udid", udid, "imagePath", imagePath, "err", mountErr)
	p, signature, err = p.signWithTss(identity, identifiers)
	reconnectedConn = p.deviceConn
	if err != nil {
		return err
	}
	return p.uploadAndMount(signature, dmgPath, trustCache)
}

// signWithTss reconnects to the image mounter service, queries a fresh nonce and gets a signature
// from Apple's TSS server. The returned mounter holds the new connection.
//
// 必须先重连：设备在一条命令失败之后会把 socket 关掉。
func (p PersonalizedDeveloperDiskImageMounter) signWithTss(identity buildIdentity, identifiers personalizationIdentifiers) (PersonalizedDeveloperDiskImageMounter, []byte, error) {
	reconnected, err := p.closeAndReconnect()
	if err != nil {
		return reconnected, nil, fmt.Errorf("MountImage: failed to reconnect before requesting a signature: %w", err)
	}

	nonce, err := reconnected.queryPersonalizedImageNonce()
	if err != nil {
		return reconnected, nil, fmt.Errorf("MountImage: failed to get nonce: %w", err)
	}

	signature, err := reconnected.tss.getSignature(identity, identifiers, nonce, reconnected.ecid)
	if err != nil {
		return reconnected, nil, fmt.Errorf("MountImage: failed to get signature from Apple: %w", err)
	}
	return reconnected, signature, nil
}

// uploadAndMount uploads the developer disk image to the device and mounts it with the given signature.
func (p PersonalizedDeveloperDiskImageMounter) uploadAndMount(signature []byte, dmgPath string, trustCache []byte) error {
	imageSize, err := getFileSize(dmgPath)
	if err != nil {
		return fmt.Errorf("MountImage: %w", err)
	}

	err = sendUploadRequest(p.plistRw, "Personalized", signature, imageSize)
	if err != nil {
		return fmt.Errorf("MountImage: failed to send upload request for image: %w", err)
	}
	imageFile, err := os.Open(dmgPath)
	if err != nil {
		return fmt.Errorf("MountImage: failed to open developer disk dmg file '%s': %w", dmgPath, err)
	}
	defer imageFile.Close()
	n, err := io.Copy(p.deviceConn.Writer(), imageFile)
	golog.Debug("bytes written", "module", logModule, "udid", p.entry.Properties.SerialNumber, "dmgPath", dmgPath, "count", n)
	if err != nil {
		return fmt.Errorf("MountImage: could not copy developer disk image to the device: %w", err)
	}
	err = waitForUploadComplete(p.plistRw)
	if err != nil {
		return err
	}

	err = p.mountPersonalizedImage(signature, trustCache)
	if err != nil {
		return fmt.Errorf("MountImage: mount command failed: %w", err)
	}

	err = hangUp(p.plistRw)
	if err != nil {
		return fmt.Errorf("MountImage: HangUp command failed: %w", err)
	}
	return nil
}

func (p PersonalizedDeveloperDiskImageMounter) UnmountImage() error {
	req := map[string]interface{}{
		"Command":   "UnmountImage",
		"MountPath": "/System/Developer",
	}
	golog.Debug("sending", "module", logModule, "udid", p.entry.Properties.SerialNumber, "request", req)
	err := p.plistRw.Write(req)
	if err != nil {
		return err
	}
	return nil
}

func (p PersonalizedDeveloperDiskImageMounter) queryPersonalizationManifest(dmgPath string) ([]byte, error) {
	f, err := os.Open(dmgPath)
	if err != nil {
		return nil, fmt.Errorf("queryPersonalizationManifest: failed to open DMG: %w", err)
	}
	defer f.Close()

	h := sha512.New384()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("queryPersonalizationManifest: failed to hash DMG: %w", err)
	}
	digest := h.Sum(nil)

	err = p.plistRw.Write(map[string]interface{}{
		"Command":               "QueryPersonalizationManifest",
		"PersonalizedImageType": "DeveloperDiskImage",
		"ImageType":             "DeveloperDiskImage",
		"ImageSignature":        digest,
	})
	if err != nil {
		return nil, fmt.Errorf("queryPersonalizationManifest: failed to write command: %w", err)
	}

	var resp map[string]interface{}
	err = p.plistRw.Read(&resp)
	if err != nil {
		return nil, fmt.Errorf("queryPersonalizationManifest: failed to read response: %w", err)
	}

	if sig, ok := resp["ImageSignature"].([]byte); ok {
		return sig, nil
	}
	return nil, fmt.Errorf("queryPersonalizationManifest: no ImageSignature in response %+v", resp)
}

func (p PersonalizedDeveloperDiskImageMounter) closeAndReconnect() (PersonalizedDeveloperDiskImageMounter, error) {
	p.deviceConn.Close()

	deviceConn, err := ios.ConnectToService(p.entry, serviceName)
	if err != nil {
		return p, fmt.Errorf("closeAndReconnect: failed to reconnect to %s: %w", serviceName, err)
	}

	return PersonalizedDeveloperDiskImageMounter{
		deviceConn: deviceConn,
		plistRw:    ios.NewPlistCodecReadWriter(deviceConn.Reader(), deviceConn.Writer()),
		version:    p.version,
		tss:        p.tss,
		ecid:       p.ecid,
		entry:      p.entry,
	}, nil
}

func (p PersonalizedDeveloperDiskImageMounter) queryPersonalizedImageNonce() ([]byte, error) {
	err := p.plistRw.Write(map[string]interface{}{
		"Command":               "QueryNonce",
		"HostProcessName":       "CoreDeviceService",
		"PersonalizedImageType": "DeveloperDiskImage",
	})
	if err != nil {
		return nil, fmt.Errorf("queryPersonalizedImageNonce: failed to write 'QueryNonce' command: %w", err)
	}

	var resp map[string]interface{}
	err = p.plistRw.Read(&resp)
	if err != nil {
		return nil, fmt.Errorf("queryPersonalizedImageNonce: failed to read response for 'QueryNonce': %w", err)
	}
	if nonce, ok := resp["PersonalizationNonce"].([]byte); ok {
		return nonce, nil
	}
	return nil, fmt.Errorf("queryPersonalizedImageNonce: could not get nonce from response %+v", resp)
}

func (p PersonalizedDeveloperDiskImageMounter) queryIdentifiers() (personalizationIdentifiers, error) {
	err := p.plistRw.Write(map[string]interface{}{
		"Command":               "QueryPersonalizationIdentifiers",
		"PersonalizedImageType": "DeveloperDiskImage",
	})
	if err != nil {
		return personalizationIdentifiers{}, fmt.Errorf("queryIdentifiers: failed to write 'QueryPersonalizationIdentifiers' command: %w", err)
	}

	var resp map[string]interface{}
	err = p.plistRw.Read(&resp)
	if err != nil {
		return personalizationIdentifiers{}, fmt.Errorf("queryIdentifiers: failed to read response for 'QueryPersonalizationIdentifiers': %w", err)
	}

	var persIdentifiers map[string]interface{}
	var ok bool
	if persIdentifiers, ok = resp["PersonalizationIdentifiers"].(map[string]interface{}); !ok {
		return personalizationIdentifiers{}, fmt.Errorf("queryIdentifiers: response has no 'PersonalizationIdentifiers' entry: %+v", resp)
	}

	identifiers := personalizationIdentifiers{
		AdditionalIdentifiers: map[string]interface{}{},
	}

	for k, v := range persIdentifiers {
		if strings.HasPrefix(k, "Ap,") {
			identifiers.AdditionalIdentifiers[k] = v
		}
	}

	if board, ok := persIdentifiers["BoardId"].(uint64); ok {
		identifiers.BoardId = int(board)
	}
	if chip, ok := persIdentifiers["ChipID"].(uint64); ok {
		identifiers.ChipID = int(chip)
	}
	if secDom, ok := persIdentifiers["SecurityDomain"].(uint64); ok {
		identifiers.SecurityDomain = int(secDom)
	}

	return identifiers, nil
}

func (p PersonalizedDeveloperDiskImageMounter) mountPersonalizedImage(signatureBytes []byte, trustCache []byte) error {
	err := p.plistRw.Write(map[string]interface{}{
		"Command":         "MountImage",
		"ImageSignature":  signatureBytes,
		"ImageType":       "Personalized",
		"ImageTrustCache": trustCache,
	})
	if err != nil {
		return fmt.Errorf("mountPersonalizedImage: failed to write 'MountImage' command: %w", err)
	}

	// 设备会对 MountImage 回结果。不检查的话，签名不匹配、镜像损坏这类拒绝
	// 会被当成挂载成功，后续所有依赖 DDI 的功能才报错，根因就查不回来了。
	var res map[string]interface{}
	err = p.plistRw.Read(&res)
	if err != nil {
		return fmt.Errorf("mountPersonalizedImage: failed to read response for 'MountImage': %w", err)
	}
	golog.Debug("received mount response", "module", logModule, "udid", p.entry.Properties.SerialNumber, "response", res)

	if deviceError, ok := res["Error"]; ok {
		return deviceRejection("mountPersonalizedImage", deviceError, res["DetailedError"])
	}
	if status, ok := res["Status"]; ok && status != "Complete" {
		return fmt.Errorf("mountPersonalizedImage: unexpected status in response: %+v", res)
	}
	return nil
}

func getFileSize(p string) (uint64, error) {
	info, err := os.Stat(p)
	if err != nil {
		return 0, fmt.Errorf("getFileSize: could not get file stats for '%s': %w", p, err)
	}
	if info.IsDir() {
		return 0, fmt.Errorf("getFileSize: expected a file, but got a directory: '%s'", p)
	}
	return uint64(info.Size()), nil
}
