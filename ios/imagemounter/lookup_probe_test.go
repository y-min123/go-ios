//go:build probe

package imagemounter

import (
	"os"
	"testing"

	"github.com/danielpaulus/go-ios/ios"
)

// 临时排查用：打印 LookupImage 在不同 ImageType 下的原始响应，
// 用来确认设备上已挂载的 personalized DDI 到底要用哪个 ImageType 才查得到。
// 运行：GO_IOS_PROBE_UDID=<udid> go test -tags probe -run TestLookupImageProbe -v ./ios/imagemounter/
func TestLookupImageProbe(t *testing.T) {
	udid := os.Getenv("GO_IOS_PROBE_UDID")
	if udid == "" {
		t.Skip("GO_IOS_PROBE_UDID not set")
	}

	device, err := ios.GetDevice(udid)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}

	for _, imageType := range []string{"Personalized", "Developer", "Cryptex", ""} {
		conn, err := ios.ConnectToService(device, serviceName)
		if err != nil {
			t.Fatalf("ConnectToService: %v", err)
		}
		rw := ios.NewPlistCodecReadWriter(conn.Reader(), conn.Writer())

		req := map[string]interface{}{"Command": "LookupImage"}
		if imageType != "" {
			req["ImageType"] = imageType
		}
		if err := rw.Write(req); err != nil {
			t.Logf("ImageType=%-13q write error: %v", imageType, err)
			conn.Close()
			continue
		}
		var resp map[string]interface{}
		if err := rw.Read(&resp); err != nil {
			t.Logf("ImageType=%-13q read error: %v", imageType, err)
			conn.Close()
			continue
		}
		summary := map[string]interface{}{}
		for k, v := range resp {
			if b, ok := v.([]interface{}); ok {
				summary[k] = len(b)
				continue
			}
			summary[k] = v
		}
		t.Logf("ImageType=%-13q -> %+v", imageType, summary)
		conn.Close()
	}

	// CopyDevices / QueryDeveloperModeStatus 之外，还看一眼这个命令的原始返回
	conn, err := ios.ConnectToService(device, serviceName)
	if err != nil {
		t.Fatalf("ConnectToService: %v", err)
	}
	defer conn.Close()
	rw := ios.NewPlistCodecReadWriter(conn.Reader(), conn.Writer())
	if err := rw.Write(map[string]interface{}{"Command": "CopyDevices"}); err != nil {
		t.Logf("CopyDevices write error: %v", err)
		return
	}
	var resp map[string]interface{}
	if err := rw.Read(&resp); err != nil {
		t.Logf("CopyDevices read error: %v", err)
		return
	}
	t.Logf("CopyDevices -> %+v", resp)
}
