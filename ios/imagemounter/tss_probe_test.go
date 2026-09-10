//go:build probe

package imagemounter

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path"
	"testing"
	"time"

	"github.com/Masterminds/semver"
	"github.com/danielpaulus/go-ios/ios"
	"github.com/google/uuid"
	"howett.net/plist"
)

// 临时排查用：对同一台设备、同一个 nonce，用不同的 TSS 请求参数组合各发一次，
// 打印 STATUS / MESSAGE，用来定位 STATUS=94 是哪一个参数造成的。
// 运行：GO_IOS_PROBE_UDID=<udid> GO_IOS_PROBE_DDI=<Restore 目录> go test -tags probe -run TestTssProbe -v ./ios/imagemounter/
func TestTssProbe(t *testing.T) {
	udid := os.Getenv("GO_IOS_PROBE_UDID")
	ddiPath := os.Getenv("GO_IOS_PROBE_DDI")
	if udid == "" || ddiPath == "" {
		t.Skip("GO_IOS_PROBE_UDID / GO_IOS_PROBE_DDI not set")
	}

	device, err := ios.GetDevice(udid)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	version, err := ios.GetProductVersion(device)
	if err != nil {
		t.Fatalf("GetProductVersion: %v", err)
	}
	t.Logf("device %s ios %s", udid, version)

	mounter, err := NewPersonalizedDeveloperDiskImageMounter(device, semver.MustParse(version.String()))
	if err != nil {
		t.Fatalf("NewPersonalizedDeveloperDiskImageMounter: %v", err)
	}
	defer mounter.Close()

	identifiers, err := mounter.queryIdentifiers()
	if err != nil {
		t.Fatalf("queryIdentifiers: %v", err)
	}
	t.Logf("identifiers: BoardId=0x%x ChipID=0x%x SecurityDomain=0x%x additional=%+v",
		identifiers.BoardId, identifiers.ChipID, identifiers.SecurityDomain, identifiers.AdditionalIdentifiers)

	nonce, err := mounter.queryPersonalizedImageNonce()
	if err != nil {
		t.Fatalf("queryPersonalizedImageNonce: %v", err)
	}
	t.Logf("nonce: %x", nonce)

	manifest, err := loadBuildManifest(path.Join(ddiPath, "BuildManifest.plist"))
	if err != nil {
		t.Fatalf("loadBuildManifest: %v", err)
	}
	identity, err := manifest.findIdentity(identifiers)
	if err != nil {
		t.Fatalf("findIdentity: %v", err)
	}
	for k, e := range identity.Manifest {
		t.Logf("manifest entry %s: Trusted=%v EPRO=%v ESEC=%v Name=%q path=%s", k, e.Trusted, e.EPRO, e.ESEC, e.Name, e.Info.Path)
	}

	base := func() map[string]interface{} {
		return map[string]interface{}{
			"@ApImg4Ticket":     true,
			"@BBTicket":         true,
			"@HostPlatformInfo": "mac",
			"ApBoardID":         identifiers.BoardId,
			"ApChipID":          identifiers.ChipID,
			"ApECID":            mounter.ecid,
			"ApNonce":           nonce,
			"ApProductionMode":  true,
			"ApSecurityDomain":  identifiers.SecurityDomain,
			"ApSecurityMode":    true,
			"SepNonce":          make([]byte, 20),
			"UID_MODE":          false,
		}
	}

	// eproEsec: 0 = 按当前代码原样带出（缺失即 false），1 = 强制 true，2 = 完全不带
	addEntries := func(params map[string]interface{}, eproEsec int) {
		for key, entry := range identity.Manifest {
			if !entry.Trusted {
				continue
			}
			e := map[string]interface{}{
				"Digest":  entry.Digest,
				"Trusted": true,
			}
			switch eproEsec {
			case 0:
				e["EPRO"] = entry.EPRO
				e["ESEC"] = entry.ESEC
			case 1:
				e["EPRO"] = true
				e["ESEC"] = true
			}
			if key == "PersonalizedDMG" || key == "PersonalizedDmg" {
				if entry.Name != "" {
					e["Name"] = entry.Name
				} else {
					e["Name"] = "DeveloperDiskImage"
				}
			}
			params[key] = e
		}
		for k, v := range identifiers.AdditionalIdentifiers {
			params[k] = v
		}
	}

	cases := []struct {
		name  string
		build func() map[string]interface{}
	}{
		{"A_current(EPRO/ESEC=false, VersionInfo=1104, UUID)", func() map[string]interface{} {
			p := base()
			p["@VersionInfo"] = "libauthinstall-1104.0.9"
			p["@UUID"] = uuid.New().String()
			addEntries(p, 0)
			return p
		}},
		{"B_EPRO/ESEC=true, VersionInfo=1104, UUID", func() map[string]interface{} {
			p := base()
			p["@VersionInfo"] = "libauthinstall-1104.0.9"
			p["@UUID"] = uuid.New().String()
			addEntries(p, 1)
			return p
		}},
		{"C_EPRO/ESEC omitted, VersionInfo=1104, UUID", func() map[string]interface{} {
			p := base()
			p["@VersionInfo"] = "libauthinstall-1104.0.9"
			p["@UUID"] = uuid.New().String()
			addEntries(p, 2)
			return p
		}},
		{"D_EPRO/ESEC=true, VersionInfo=973.40.2, no UUID (old working code)", func() map[string]interface{} {
			p := base()
			p["@VersionInfo"] = "libauthinstall-973.40.2"
			addEntries(p, 1)
			return p
		}},
		{"E_EPRO/ESEC=false, VersionInfo=973.40.2, no UUID", func() map[string]interface{} {
			p := base()
			p["@VersionInfo"] = "libauthinstall-973.40.2"
			addEntries(p, 0)
			return p
		}},
	}

	for _, c := range cases {
		status, message, ticketLen, err := probeTss(c.build())
		if err != nil {
			t.Logf("%-60s -> transport error: %v", c.name, err)
			continue
		}
		t.Logf("%-60s -> STATUS=%d MESSAGE=%q ticketBytes=%d", c.name, status, message, ticketLen)
	}
}

func probeTss(params map[string]interface{}) (int, string, int, error) {
	buf := bytes.NewBuffer(nil)
	if err := plist.NewEncoderForFormat(buf, plist.XMLFormat).Encode(params); err != nil {
		return 0, "", 0, err
	}
	h := http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		Timeout:   1 * time.Minute,
	}
	req, err := http.NewRequest("POST", "https://gs.apple.com/TSS/controller?action=2", buf)
	if err != nil {
		return 0, "", 0, err
	}
	res, err := h.Do(req)
	if err != nil {
		return 0, "", 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, "", 0, fmt.Errorf("http status %d", res.StatusCode)
	}
	resp, err := parseResponse(res.Body)
	if err != nil {
		return 0, "", 0, err
	}
	ticketLen := 0
	if resp.status == 0 {
		var ticket map[string]interface{}
		if _, err := plist.Unmarshal([]byte(resp.requestString), &ticket); err == nil {
			if t, ok := ticket["ApImg4Ticket"].([]byte); ok {
				ticketLen = len(t)
			}
		}
	}
	return resp.status, resp.message, ticketLen, nil
}
