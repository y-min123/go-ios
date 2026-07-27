package main

/*
#cgo CFLAGS: -mmacosx-version-min=10.15
#cgo LDFLAGS: -mmacosx-version-min=10.15
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/imagemounter"
	"github.com/danielpaulus/go-ios/ios/instruments"
	"github.com/danielpaulus/go-ios/ios/simlocation"
	"github.com/danielpaulus/go-ios/ios/tunnel"
)

var (
	lastErrorMu sync.Mutex
	lastError   string

	locationSessionMu sync.Mutex
	locationSessions  = map[string]*instruments.LocationSimulationService{}

	deviceListenOnce   sync.Once
	deviceEvents       chan []byte
	deviceListenCancel context.CancelFunc

	tunnelInfoCacheMu sync.Mutex
	tunnelInfoCache   = map[string]tunnelInfoCacheEntry{}

	deviceVersionCacheMu sync.Mutex
	deviceVersionCache   = map[string]deviceVersionCacheEntry{}

	locationTimeoutMu sync.RWMutex
	locationTimeout   time.Duration
)

type tunnelInfoCacheEntry struct {
	info        tunnel.Tunnel
	lastSuccess time.Time
	lastFailure time.Time
}

type deviceVersionCacheEntry struct {
	major       int
	ok          bool
	lastChecked time.Time
}

const (
	tunnelInfoMaxAge         = 30 * time.Second
	tunnelInfoFailureBackoff = 2 * time.Second
	deviceVersionMaxAge      = 5 * time.Minute
	devModeCheckMaxAttempts  = 3
	devModeCheckRetryDelay   = 350 * time.Millisecond
)

// main is required for -buildmode=c-shared though it remains unused.
func main() {}

//export GoIOSSetLocationTimeoutMs
func GoIOSSetLocationTimeoutMs(timeoutMs C.int) {
	if timeoutMs <= 0 {
		setLocationTimeout(0)
		instruments.SetLocationSimulationTimeout(0)
		return
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	setLocationTimeout(timeout)
	instruments.SetLocationSimulationTimeout(timeout)
}

//export GoIOSIsDevModeEnabled
func GoIOSIsDevModeEnabled(cUdid *C.char) C.int {
	udid := strings.TrimSpace(C.GoString(cUdid))
	if udid == "" {
		udid = os.Getenv("GO_IOS_UDID")
	}
	enabled, err := queryDeveloperModeStatus(udid)
	if err != nil {
		return storeError(err)
	}

	storeError(nil)
	if enabled {
		return 1
	}
	return 0
}

//export GoIOSIsDeviceLocked
func GoIOSIsDeviceLocked(cUdid *C.char) C.int {
	device, err := prepareDevice(C.GoString(cUdid))
	if err != nil {
		return storeError(err)
	}

	lockdownConnection, err := ios.ConnectLockdownWithSession(device)
	if err != nil {
		return storeError(err)
	}
	defer lockdownConnection.Close()

	passwordProtectedIntf, err := lockdownConnection.GetValue("PasswordProtected")
	if err != nil {
		return storeError(err)
	}

	passwordProtected, ok := passwordProtectedIntf.(bool)
	if !ok {
		return storeError(errors.New("无法将 PasswordProtected 转换为布尔值"))
	}

	storeError(nil)
	if passwordProtected {
		return 1
	}
	return 0
}

//export GoIOSIsDeviceTrusted
func GoIOSIsDeviceTrusted(cUdid *C.char) C.int {
	udid := strings.TrimSpace(C.GoString(cUdid))
	if udid == "" {
		udid = os.Getenv("GO_IOS_UDID")
	}

	device, err := ios.GetDevice(udid)
	if err != nil {
		return storeError(err)
	}

	if _, err := ios.GetValues(device); err != nil {
		storeError(nil)
		return 0
	}

	storeError(nil)
	return 1
}

//export GoIOSSetLocation
func GoIOSSetLocation(cUdid *C.char, lat C.double, lon C.double) C.int {
	err := setLocationWithTimeout(C.GoString(cUdid), float64(lat), float64(lon))
	return storeError(err)
}

//export GoIOSSetLocationGPX
func GoIOSSetLocationGPX(cUdid *C.char, cFilePath *C.char) C.int {
	device, err := prepareDevice(C.GoString(cUdid))
	if err != nil {
		return storeError(err)
	}

	if device.SupportsRsd() {
		return storeError(errors.New("GPX playback via RSD is not supported; call GoIOSSetLocation repeatedly instead"))
	}

	filePath := strings.TrimSpace(C.GoString(cFilePath))
	if filePath == "" {
		return storeError(errors.New("gpx file path must not be empty"))
	}

	return storeError(simlocation.SetLocationGPX(device, filePath))
}

//export GoIOSResetLocation
func GoIOSResetLocation(cUdid *C.char) C.int {
	device, err := prepareDevice(C.GoString(cUdid))
	if err != nil {
		return storeError(err)
	}

	if device.SupportsRsd() {
		return storeError(stopSimulationSession(device))
	}

	return storeError(simlocation.ResetLocation(device))
}

//export GoIOSGetLastError
func GoIOSGetLastError() *C.char {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	return C.CString(lastError)
}

//export GoIOSListDevices
func GoIOSListDevices() *C.char {
	deviceList, err := ios.ListDevices()
	if err != nil {
		storeError(err)
		return nil
	}

	type deviceInfo struct {
		Udid             string `json:"udid"`
		ConnectionType   string `json:"connectionType"`
		Name             string `json:"name,omitempty"`
		ProductType      string `json:"productType,omitempty"`
		ProductVersion   string `json:"productVersion,omitempty"`
		DeviceID         int    `json:"deviceId,omitempty"`
		BluetoothAddress string `json:"bluetoothAddress,omitempty"`
	}

	devices := make([]deviceInfo, 0, len(deviceList.DeviceList))
	for _, entry := range deviceList.DeviceList {
		info := deviceInfo{
			Udid:           entry.Properties.SerialNumber,
			ConnectionType: entry.Properties.ConnectionType,
		}

		if entry.Properties.SerialNumber != "" {
			device, err := ios.GetDevice(entry.Properties.SerialNumber)
			if err == nil {
				if values, err := ios.GetValues(device); err == nil {
					info.Name = values.Value.DeviceName
					info.ProductType = values.Value.ProductType
					info.ProductVersion = values.Value.ProductVersion
					info.BluetoothAddress = values.Value.BluetoothAddress
				} else {
					// 未信任设备：GetValues 需要配对记录，fallback 到无 session 的 lockdown 查询
					if name, err := getDeviceNameWithoutSession(entry.DeviceID); err == nil {
						info.Name = name
					}
				}
			}
		}
		info.DeviceID = entry.DeviceID
		devices = append(devices, info)
	}

	payload, err := json.Marshal(devices)
	if err != nil {
		storeError(err)
		return nil
	}

	storeError(nil)
	return C.CString(string(payload))
}

//export GoIOSNextDeviceEvent
func GoIOSNextDeviceEvent(timeoutMs C.int) *C.char {
	ensureDeviceListener()

	var timeout <-chan time.Time
	if timeoutMs > 0 {
		timeout = time.After(time.Duration(timeoutMs) * time.Millisecond)
	}

	select {
	case evt := <-deviceEvents:
		storeError(nil)
		return C.CString(string(evt))
	case <-timeout:
		storeError(errors.New("timeout waiting for device event"))
		return nil
	}
}

//export GoIOSStopDeviceListener
func GoIOSStopDeviceListener() {
	if deviceListenCancel != nil {
		deviceListenCancel()
		deviceListenCancel = nil
	}
	deviceListenOnce = sync.Once{}
	deviceEvents = nil
	storeError(nil)
}

func setLocationTimeout(timeout time.Duration) {
	locationTimeoutMu.Lock()
	locationTimeout = timeout
	locationTimeoutMu.Unlock()
}

func getLocationTimeout() time.Duration {
	locationTimeoutMu.RLock()
	defer locationTimeoutMu.RUnlock()
	return locationTimeout
}

func setLocationWithTimeout(rawUdid string, lat float64, lon float64) error {
	timeout := getLocationTimeout()
	if timeout <= 0 {
		return doSetLocation(rawUdid, lat, lon)
	}

	done := make(chan error, 1)
	go func() {
		done <- doSetLocation(rawUdid, lat, lon)
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("location set timeout after %s", timeout)
	}
}

func doSetLocation(rawUdid string, lat float64, lon float64) error {
	device, err := prepareDevice(rawUdid)
	if err != nil {
		return err
	}

	if device.SupportsRsd() {
		return startSimulationSession(device, lat, lon)
	}

	return simlocation.SetLocation(device, formatCoordinate(C.double(lat)), formatCoordinate(C.double(lon)))
}

func cachedTunnelInfo(udid string) (tunnel.Tunnel, bool) {
	tunnelInfoCacheMu.Lock()
	defer tunnelInfoCacheMu.Unlock()

	entry, ok := tunnelInfoCache[udid]
	if !ok || entry.lastSuccess.IsZero() {
		return tunnel.Tunnel{}, false
	}
	if time.Since(entry.lastSuccess) > tunnelInfoMaxAge {
		return tunnel.Tunnel{}, false
	}
	return entry.info, true
}

func markTunnelInfoSuccess(udid string, info tunnel.Tunnel) {
	tunnelInfoCacheMu.Lock()
	defer tunnelInfoCacheMu.Unlock()
	slog.Info("[TunnelCache] 标记tunnel info获取成功，写入缓存", "udid", udid)
	tunnelInfoCache[udid] = tunnelInfoCacheEntry{
		info:        info,
		lastSuccess: time.Now(),
		lastFailure: time.Time{},
	}
}

func markTunnelInfoFailure(udid string) {
	tunnelInfoCacheMu.Lock()
	defer tunnelInfoCacheMu.Unlock()
	slog.Warn("[TunnelCache] 标记tunnel info获取失败，进入退避状态", "udid", udid)
	entry := tunnelInfoCache[udid]
	entry.info = tunnel.Tunnel{}
	entry.lastSuccess = time.Time{}
	entry.lastFailure = time.Now()
	tunnelInfoCache[udid] = entry
}

func shouldSkipTunnelInfoAttempt(udid string) bool {
	tunnelInfoCacheMu.Lock()
	defer tunnelInfoCacheMu.Unlock()
	entry, ok := tunnelInfoCache[udid]
	if !ok || entry.lastFailure.IsZero() {
		return false
	}
	shouldSkip := time.Since(entry.lastFailure) < tunnelInfoFailureBackoff
	if shouldSkip {
		slog.Warn("[TunnelCache] 跳过tunnel info获取（仍在退避期内）", "udid", udid, "time_since_failure", time.Since(entry.lastFailure))
	}
	return shouldSkip
}

func clearTunnelCache(udid string) {
	tunnelInfoCacheMu.Lock()
	defer tunnelInfoCacheMu.Unlock()
	slog.Info("[TunnelCache] 清除设备的tunnel缓存", "udid", udid)
	delete(tunnelInfoCache, udid)
}

//export GoIOSClearTunnelCache
func GoIOSClearTunnelCache(cUdid *C.char) {
	udid := C.GoString(cUdid)
	slog.Info("[TunnelCache] 收到清除缓存请求", "udid", udid)
	clearTunnelCache(udid)
}

func getDeviceMajorVersion(device ios.DeviceEntry) (int, bool) {
	udid := device.Properties.SerialNumber
	if udid == "" {
		return 0, false
	}
	now := time.Now()

	deviceVersionCacheMu.Lock()
	entry, ok := deviceVersionCache[udid]
	if ok && now.Sub(entry.lastChecked) < deviceVersionMaxAge {
		deviceVersionCacheMu.Unlock()
		return entry.major, entry.ok
	}
	deviceVersionCacheMu.Unlock()

	version, err := ios.GetProductVersion(device)
	entry = deviceVersionCacheEntry{
		lastChecked: now,
	}
	if err != nil {
		entry.ok = false
		deviceVersionCacheMu.Lock()
		deviceVersionCache[udid] = entry
		deviceVersionCacheMu.Unlock()
		return 0, false
	}

	entry.major = int(version.Major())
	entry.ok = true
	deviceVersionCacheMu.Lock()
	deviceVersionCache[udid] = entry
	deviceVersionCacheMu.Unlock()
	return entry.major, true
}

func prepareDevice(rawUdid string) (ios.DeviceEntry, error) {
	udid := strings.TrimSpace(rawUdid)
	if udid == "" {
		udid = os.Getenv("GO_IOS_UDID")
	}
	slog.Info("[PrepareDevice] 开始准备设备", "udid", udid)
	device, err := ios.GetDevice(udid)
	if err != nil {
		slog.Error("[PrepareDevice] 获取设备失败", "udid", udid, "error", err)
		return ios.DeviceEntry{}, err
	}

	if info, ok := cachedTunnelInfo(device.Properties.SerialNumber); ok {
		slog.Info("[PrepareDevice] 使用缓存的tunnel info", "udid", device.Properties.SerialNumber)
		device.UserspaceTUN = info.UserspaceTUN
		device.UserspaceTUNHost = ios.HttpApiHost()
		device.UserspaceTUNPort = info.UserspaceTUNPort
		deviceWithRsd, err := attachRsdProvider(device, device.Properties.SerialNumber, info.Address, info.RsdPort)
		if err == nil {
			slog.Info("[PrepareDevice] 使用缓存成功", "udid", device.Properties.SerialNumber)
			return deviceWithRsd, nil
		}
		slog.Warn("[PrepareDevice] 缓存的tunnel info失效", "udid", device.Properties.SerialNumber, "error", err)
		markTunnelInfoFailure(device.Properties.SerialNumber)
	}

	if shouldSkipTunnelInfoAttempt(device.Properties.SerialNumber) {
		slog.Warn("[PrepareDevice] 跳过tunnel info获取（失败退避中）", "udid", device.Properties.SerialNumber)
		if major, ok := getDeviceMajorVersion(device); ok {
			if major >= 17 {
				slog.Error("[PrepareDevice] iOS 17+设备但tunnel info不可用", "udid", device.Properties.SerialNumber, "ios_version", major)
				return ios.DeviceEntry{}, fmt.Errorf("tunnel info unavailable for iOS %d device", major)
			}
			return device, nil
		}
		return ios.DeviceEntry{}, fmt.Errorf("tunnel info unavailable and device version unknown")
	}

	slog.Info("[PrepareDevice] 尝试获取tunnel info", "udid", device.Properties.SerialNumber)
	info, err := tunnel.TunnelInfoForDevice(device.Properties.SerialNumber, ios.HttpApiHost(), ios.HttpApiPort())
	if err != nil {
		slog.Error("[PrepareDevice] 获取tunnel info失败", "udid", device.Properties.SerialNumber, "error", err)
		markTunnelInfoFailure(device.Properties.SerialNumber)
		if major, ok := getDeviceMajorVersion(device); ok {
			if major >= 17 {
				return ios.DeviceEntry{}, fmt.Errorf("tunnel info unavailable for iOS %d device", major)
			}
			return device, nil
		}
		return ios.DeviceEntry{}, fmt.Errorf("tunnel info unavailable and device version unknown")
	}

	slog.Info("[PrepareDevice] 获取tunnel info成功", "udid", device.Properties.SerialNumber)
	markTunnelInfoSuccess(device.Properties.SerialNumber, info)
	device.UserspaceTUN = info.UserspaceTUN
	device.UserspaceTUNHost = ios.HttpApiHost()
	device.UserspaceTUNPort = info.UserspaceTUNPort

	return attachRsdProvider(device, device.Properties.SerialNumber, info.Address, info.RsdPort)
}

func attachRsdProvider(device ios.DeviceEntry, udid string, address string, rsdPort int) (ios.DeviceEntry, error) {
	rsdService, err := ios.NewWithAddrPortDevice(address, rsdPort, device)
	if err != nil {
		return ios.DeviceEntry{}, fmt.Errorf("could not connect to RSD at %s:%d: %w", address, rsdPort, err)
	}
	defer rsdService.Close()

	rsdProvider, err := rsdService.Handshake()
	if err != nil {
		return ios.DeviceEntry{}, fmt.Errorf("failed to handshake with RSD: %w", err)
	}

	deviceWithRsd, err := ios.GetDeviceWithAddress(udid, address, rsdProvider)
	if err != nil {
		return ios.DeviceEntry{}, err
	}
	deviceWithRsd.UserspaceTUN = device.UserspaceTUN
	deviceWithRsd.UserspaceTUNHost = device.UserspaceTUNHost
	deviceWithRsd.UserspaceTUNPort = device.UserspaceTUNPort

	return deviceWithRsd, nil
}

func startSimulationSession(device ios.DeviceEntry, lat float64, lon float64) error {
	sessionKey := sessionKey(device)

	locationSessionMu.Lock()
	defer locationSessionMu.Unlock()

	if current, ok := locationSessions[sessionKey]; ok {
		if err := current.StartSimulateLocation(lat, lon); err == nil {
			return nil
		}
		current.Close()
		delete(locationSessions, sessionKey)
	}

	service, err := instruments.NewLocationSimulationService(device)
	if err != nil {
		return err
	}

	if err := service.StartSimulateLocation(lat, lon); err != nil {
		service.Close()
		return err
	}

	locationSessions[sessionKey] = service
	return nil
}

func stopSimulationSession(device ios.DeviceEntry) error {
	sessionKey := sessionKey(device)

	locationSessionMu.Lock()
	defer locationSessionMu.Unlock()

	service, ok := locationSessions[sessionKey]
	if !ok {
		return errors.New("no active location simulation session to stop")
	}

	defer service.Close()
	delete(locationSessions, sessionKey)
	return service.StopSimulateLocation()
}

func sessionKey(device ios.DeviceEntry) string {
	return device.Properties.SerialNumber
}

func formatCoordinate(value C.double) string {
	return strconv.FormatFloat(float64(value), 'f', 6, 64)
}

func queryDeveloperModeStatus(udid string) (bool, error) {
	var lastErr error
	sawAnySuccessfulFalse := false

	for attempt := 0; attempt < devModeCheckMaxAttempts; attempt++ {
		devices, err := getDeveloperModeCandidateDevices(udid)
		if err != nil {
			lastErr = err
		} else {
			sawSuccessfulFalse := false

			for _, device := range devices {
				enabled, checkErr := imagemounter.IsDevModeEnabled(device)
				if checkErr != nil {
					lastErr = fmt.Errorf(
						"IsDevModeEnabled: transport=%s deviceID=%d err=%w",
						device.Properties.ConnectionType,
						device.DeviceID,
						checkErr,
					)
					continue
				}

				if enabled {
					return true, nil
				}

				sawSuccessfulFalse = true
				sawAnySuccessfulFalse = true
			}

			if sawSuccessfulFalse && attempt == devModeCheckMaxAttempts-1 {
				return false, nil
			}
		}

		if attempt < devModeCheckMaxAttempts-1 {
			time.Sleep(devModeCheckRetryDelay)
		}
	}

	if lastErr != nil {
		return false, lastErr
	}

	if sawAnySuccessfulFalse {
		return false, nil
	}

	return false, errors.New("IsDevModeEnabled: no usable device transport found")
}

func getDeveloperModeCandidateDevices(udid string) ([]ios.DeviceEntry, error) {
	deviceList, err := ios.ListDevices()
	if err != nil {
		return nil, err
	}

	if udid == "" {
		if len(deviceList.DeviceList) == 0 {
			return nil, errors.New("no iOS devices are attached to this host")
		}
		return []ios.DeviceEntry{deviceList.DeviceList[0]}, nil
	}

	devices := make([]ios.DeviceEntry, 0, 2)
	for _, device := range deviceList.DeviceList {
		if device.Properties.SerialNumber == udid {
			devices = append(devices, device)
		}
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("Device '%s' not found. Is it attached to the machine?", udid)
	}

	sort.SliceStable(devices, func(i, j int) bool {
		return deviceConnectionPriority(devices[i]) < deviceConnectionPriority(devices[j])
	})

	return devices, nil
}

func deviceConnectionPriority(device ios.DeviceEntry) int {
	if strings.EqualFold(device.Properties.ConnectionType, "Network") {
		return 1
	}
	return 0
}

// getDeviceNameWithoutSession 通过无 session 的 lockdown 连接查询设备名称。
// 适用于未信任设备（iOS 16 以下可正常返回，iOS 16+ 可能返回空）。
func getDeviceNameWithoutSession(deviceID int) (string, error) {
	muxConn, err := ios.NewUsbMuxConnectionSimple()
	if err != nil {
		return "", err
	}
	defer muxConn.Close()

	lockdownConn, err := muxConn.ConnectLockdown(deviceID)
	if err != nil {
		return "", err
	}
	defer lockdownConn.Close()

	val, err := lockdownConn.GetValue("DeviceName")
	if err != nil {
		return "", err
	}
	name, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("DeviceName 类型断言失败")
	}
	return name, nil
}

func storeError(err error) C.int {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()

	if err != nil {
		lastError = err.Error()
		return -1
	}

	lastError = ""
	return 0
}

func ensureDeviceListener() {
	deviceListenOnce.Do(func() {
		deviceEvents = make(chan []byte, 16)
		ctx, cancel := context.WithCancel(context.Background())
		deviceListenCancel = cancel
		go listenForDeviceEvents(ctx)
	})
}

func listenForDeviceEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		deviceConn, err := ios.NewDeviceConnection(ios.GetUsbmuxdSocket())
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}

		muxConnection := ios.NewUsbMuxConnection(deviceConn)
		receiver, err := muxConnection.Listen()
		if err != nil {
			deviceConn.Close()
			time.Sleep(3 * time.Second)
			continue
		}

		for {
			select {
			case <-ctx.Done():
				deviceConn.Close()
				return
			default:
			}

			msg, err := receiver()
			if err != nil {
				deviceConn.Close()
				time.Sleep(time.Second)
				break
			}

			payload, err := json.Marshal(msg)
			if err != nil {
				continue
			}

			select {
			case deviceEvents <- payload:
			default:
				<-deviceEvents
				deviceEvents <- payload
			}
		}
	}
}
