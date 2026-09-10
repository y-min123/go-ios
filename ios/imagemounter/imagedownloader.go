package imagemounter

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver"
	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/golog"
)

var (
	versionMap = map[string]string{
		"4.2":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/4.2",
		"4.3":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/4.3",
		"5.0":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/5.0",
		"5.1":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/5.1",
		"6.0":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/6.0",
		"6.1":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/6.1",
		"7.0":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/7.0",
		"7.1":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/7.1",
		"8.0":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/8.0",
		"8.1":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/8.1",
		"8.2":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/8.2",
		"8.3":             "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/8.3",
		"8.4 (12H141)":    "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/8.4%20(12H141)",
		"9.0 (13A340)":    "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/9.0%20(13A340)",
		"9.1 (13B5110e)":  "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/9.1%20(13B5110e)",
		"9.2 (13C75)":     "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/9.2%20(13C75)",
		"9.3 (13E230)":    "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/9.3%20(13E230)",
		"10.0 (14A345)":   "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/10.0%20(14A345)",
		"10.1 (14B72)":    "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/10.1%20(14B72)",
		"10.2 (14C5062c)": "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/10.2%20(14C5062c)",
		"10.3 (14E269)":   "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/10.3%20(14E269)",
		"11.0 (15A372)":   "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/11.0%20(15A372)",
		"11.1 (15B87)":    "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/11.1%20(15B87)",
		"11.2 (15C5092b)": "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/11.2%20(15C5092b)",
		"11.3 (15E5178d)": "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/11.3%20(15E5178d)",
		"11.4 (15F5037c)": "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/11.4%20(15F5037c)",
		"12.0 (16A5288q)": "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/12.0%20(16A5288q)",
		"12.1 (16B5059d)": "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/12.1%20(16B5059d)",
		"12.2 (16E5191d)": "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/12.2%20(16E5191d)",
		"12.3 (16F148)":   "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/12.3%20(16F148)",
		"12.4":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/12.4",
		"13.0":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/13.0",
		"13.1":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/13.1",
		"13.2":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/13.2",
		"13.3":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/13.3",
		"13.4":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/13.4",
		"13.5":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/13.5",
		"13.7":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/13.7",
		"14.0":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/14.0",
		"14.1":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/14.1",
		"14.2":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/14.2",
		"14.4":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/14.4",
		"14.5":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/14.5",
		"14.6":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/14.6",
		"14.7":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/14.7",
		"14.7.1":          "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/14.7.1",
		"14.8":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/14.8",
		"15.0":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/15.0",
		"15.1":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/15.1",
		"15.2":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/15.2",
		"15.3":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/15.3",
		"15.3.1":          "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/15.3.1",
		"15.4":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/15.4",
		"15.5":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/15.5",
		"15.6":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/15.6",
		"15.6.1":          "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/15.6.1",
		"15.7":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/15.7",
		"16.0":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/16.0",
		"16.1":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/16.1",
		"16.2":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/16.2",
		"16.3":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/16.3",
		"16.4":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/16.4",
		"16.4.1":          "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/16.4.1",
		"16.5":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/16.5",
		"16.6":            "https://github.com/mspvirajpatel/Xcode_Developer_Disk_Images/blob/master/Developer%20Disk%20Image/16.6",
	}

	availableVersions = []string{"4.2", "4.3", "5.0", "5.1", "6.0", "6.1", "7.0", "7.1", "8.0", "8.1", "8.2", "8.3", "8.4 (12H141)", "9.0 (13A340)", "9.1 (13B5110e)", "9.2 (13C75)", "9.3 (13E230)", "10.0 (14A345)", "10.1 (14B72)", "10.2 (14C5062c)", "10.3 (14E269)", "11.0 (15A372)", "11.1 (15B87)", "11.2 (15C5092b)", "11.3 (15E5178d)", "11.4 (15F5037c)", "12.0 (16A5288q)", "12.1 (16B5059d)", "12.2 (16E5191d)", "12.3 (16F148)", "12.4", "13.0", "13.1", "13.2", "13.3", "13.4", "13.5", "13.7", "14.0", "14.1", "14.2", "14.4", "14.5", "14.6", "14.7", "14.7.1", "14.8", "15.0", "15.1", "15.2", "15.3.1", "15.3", "15.4", "15.5", "15.6", "15.6.1", "15.7", "16.0", "16.1", "16.2", "16.3", "16.4", "16.4.1", "16.5", "16.6"}
)

const (
	imageFile     = "DeveloperDiskImage.dmg"
	signatureFile = "DeveloperDiskImage.dmg.signature"
	devicebox     = "https://deviceboxhq.com/"
	// iOS 17+ 的 personalized DDI，托管在 deviceboxhq。
	// 一份通吃所有 iOS 17+ 设备：镜像本身不区分系统版本，BuildManifest 覆盖
	// 0x8010 ~ 0x8150 全部芯片，比上一份 ddi-15F31d 多出 A18 / A19（iPhone 16 / 17）。
	// 有新版本发布时直接 bump。
	personalizedDDI = "ddi-17E5179g"
	// iOS 17+ 的镜像解压后，DownloadImageFor 返回的是这个子目录
	restoreDirName = "Restore"
)

// DiscardCachedImage removes the cached developer disk image that imagePath belongs to,
// so the next run downloads it again. imagePath is what DownloadImageFor returned:
// a DeveloperDiskImage.dmg file for iOS < 17, or a "Restore" directory for iOS 17+.
//
// 用于挂载失败之后。缓存的完整性只能靠"能不能挂上"来判断，
// 内容损坏但文件齐全的镜像不丢弃的话，后续每次连接都会用同一份坏缓存重复失败。
func DiscardCachedImage(imagePath string) error {
	if imagePath == "" {
		return nil
	}

	// 两种形态取一次 Dir 都能落到缓存目录：
	// <baseDir>/15.7/DeveloperDiskImage.dmg -> <baseDir>/15.7
	// <baseDir>/ddi-17E5179g/Restore        -> <baseDir>/ddi-17E5179g
	cacheDir := filepath.Dir(imagePath)

	// 兜住异常入参，避免把 baseDir 或其它版本的缓存删掉
	if cleaned := filepath.Clean(cacheDir); cleaned == "." || cleaned == string(filepath.Separator) {
		return fmt.Errorf("DiscardCachedImage: refusing to remove suspicious cache directory '%s'", cacheDir)
	}

	golog.Warn("discarding cached developer disk image, it will be downloaded again on the next run", "module", logModule, "imagePath", imagePath, "path", cacheDir)
	if err := os.RemoveAll(cacheDir); err != nil {
		return fmt.Errorf("DiscardCachedImage: failed removing '%s': %w", cacheDir, err)
	}

	// iOS 17+ 的下载产物还包含与解压目录同名的 zip，一并删除避免留下孤儿文件
	if err := os.Remove(cacheDir + ".zip"); err != nil && !os.IsNotExist(err) {
		golog.Warn("failed removing cached developer disk image archive", "module", logModule, "path", cacheDir+".zip", "err", err)
	}
	return nil
}

func MatchAvailable(version string) string {
	golog.Debug("matching available image for device version", "module", logModule, "version", version)
	requestedVersionParsed := semver.MustParse(version)
	var bestMatch *semver.Version = nil
	var bestMatchString string

	for _, availableVersion := range availableVersions {
		parsedAV := semver.MustParse(strings.Split(availableVersion, " (")[0])
		if parsedAV.Equal(requestedVersionParsed) {
			return availableVersion
		}
		if bestMatch == nil {
			bestMatch = parsedAV
			bestMatchString = availableVersion
			continue
		}
		if parsedAV.GreaterThan(bestMatch) && (parsedAV.LessThan(requestedVersionParsed)) {
			bestMatch = parsedAV
			bestMatchString = availableVersion
		}
	}
	golog.Debug("matched available image", "module", logModule, "version", version, "bestMatch", bestMatch)

	return bestMatchString
}

func Download17Plus(baseDir string, version *semver.Version) (string, error) {
	downloadUrl := fmt.Sprintf("%s%s%s", devicebox, personalizedDDI, ".zip")
	golog.Info("getting developer image", "module", logModule, "version", version.String(), "ddi", personalizedDDI, "url", downloadUrl)

	imageDownloaded, err := validateBaseDirAndLookForImage(baseDir, personalizedDDI)
	if err != nil {
		return "", err
	}
	if imageDownloaded != "" {
		golog.Info("using already downloaded image", "module", logModule, "path", imageDownloaded)
		return path.Join(imageDownloaded, restoreDirName), err
	}
	imageFileName := path.Join(baseDir, personalizedDDI+".zip")
	extractedPath := path.Join(baseDir, personalizedDDI)
	golog.Info("downloading image", "module", logModule, "url", downloadUrl, "path", imageFileName)
	err = downloadFile(imageFileName, downloadUrl)
	if err != nil {
		return "", err
	}
	_, _, err = ios.Unzip(imageFileName, extractedPath)
	if err != nil {
		return "", fmt.Errorf("Download17Plus: error extracting image %s %w", imageFileName, err)
	}

	return path.Join(extractedPath, restoreDirName), nil
}

func DownloadImageFor(device ios.DeviceEntry, baseDir string) (string, error) {
	allValues, err := ios.GetValues(device)
	if err != nil {
		return "", err
	}
	parsedVersion, err := semver.NewVersion(allValues.Value.ProductVersion)
	if err != nil {
		return "", fmt.Errorf("DownloadImageFor: failed parsing ios productversion: '%s' with %w", allValues.Value.ProductVersion, err)
	}
	if parsedVersion.GreaterThan(ios.IOS17()) || parsedVersion.Equal(ios.IOS17()) {
		return Download17Plus(baseDir, parsedVersion)
	}
	version := MatchAvailable(allValues.Value.ProductVersion)
	golog.Info("getting developer image", "module", logModule, "udid", device.Properties.SerialNumber, "version", allValues.Value.ProductVersion, "imageVersion", version)
	var imageToFind string
	switch runtime.GOOS {
	case "windows":
		imageToFind = fmt.Sprintf("%s\\%s", version, imageFile)
	default:
		imageToFind = fmt.Sprintf("%s/%s", version, imageFile)
	}
	imageDownloaded, err := validateBaseDirAndLookForImage(baseDir, imageToFind)
	if err != nil {
		return "", err
	}
	if imageDownloaded != "" {
		// findImage 只按文件名匹配，signature 缺失同样会让挂载失败，此时要丢弃缓存重新下载
		if _, statErr := os.Stat(imageDownloaded + ".signature"); statErr != nil {
			golog.Warn("cached developer disk image is incomplete, signature file is missing. discarding cache and downloading again", "module", logModule, "udid", device.Properties.SerialNumber, "path", imageDownloaded, "err", statErr)
			if rmErr := os.Remove(imageDownloaded); rmErr != nil {
				return "", fmt.Errorf("DownloadImageFor: failed removing incomplete image '%s': %w", imageDownloaded, rmErr)
			}
		} else {
			golog.Info("image already downloaded from https://github.com/mspvirajpatel/", "module", logModule, "udid", device.Properties.SerialNumber, "path", imageDownloaded)
			return imageDownloaded, nil
		}
	}
	golog.Info("thank you github.com/mspvirajpatel for making these images available :-)", "module", logModule, "udid", device.Properties.SerialNumber)
	versionDir := strings.Split(version, " (")[0]
	downloadUrl := versionMap[version] + "/" + imageFile + "?raw=true"
	imageFileName := path.Join(baseDir, versionDir, imageFile)

	signatureDownloadUrl := versionMap[version] + "/" + signatureFile + "?raw=true"
	signatureFileName := path.Join(baseDir, versionDir, signatureFile)
	// 用 MkdirAll 而不是 Mkdir：目录已存在时 Mkdir 会报 EEXIST，
	// 上一次下载失败留下的空目录会让后续每一次下载都在这里直接失败
	err = os.MkdirAll(path.Join(baseDir, versionDir), 0o755)
	if err != nil {
		return "", fmt.Errorf("DownloadImageFor: failed creating image directory '%s': %w", path.Join(baseDir, versionDir), err)
	}
	golog.Info("downloading developer disk image", "module", logModule, "udid", device.Properties.SerialNumber, "url", downloadUrl, "path", imageFileName)
	err = downloadFile(imageFileName, downloadUrl)
	if err != nil {
		return "", fmt.Errorf("DownloadImageFor: failed downloading image for ios %s: %w", allValues.Value.ProductVersion, err)
	}

	golog.Info("downloading developer disk image signature", "module", logModule, "udid", device.Properties.SerialNumber, "url", signatureDownloadUrl, "path", signatureFileName)
	err = downloadFile(signatureFileName, signatureDownloadUrl)
	if err != nil {
		return "", fmt.Errorf("DownloadImageFor: failed downloading image signature for ios %s: %w", allValues.Value.ProductVersion, err)
	}

	return imageFileName, nil
}

func findImage(dir string, imageToFind string) (string, error) {
	var imageWeFound string
	err := filepath.Walk(dir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if strings.HasSuffix(path, imageToFind) {
				imageWeFound = path
			}
			return nil
		})
	if err != nil {
		return "", err
	}
	if imageWeFound != "" {
		return imageWeFound, nil
	}
	return "", fmt.Errorf("image not found")
}

func validateBaseDirAndLookForImage(baseDir string, imageToFind string) (string, error) {
	dirHandle, err := os.Open(baseDir)
	defer dirHandle.Close()
	if err != nil {
		err := os.MkdirAll(baseDir, 0o777)
		if err != nil {
			return "", err
		}
		return "", nil
	}

	dmgPath, err := findImage(baseDir, imageToFind)
	if err != nil {
		return "", nil
	}

	return dmgPath, nil
}

// DownloadFile will download a url to a local file. It's efficient because it will
// write as it downloads and not load the whole file into memory.
// PS: Taken from golangcode.com
//
// 下载先落到 .tmp 再重命名。网络中断留下的半截文件如果直接用最终文件名落盘，
// 后续会被 findImage 按文件名当成有效缓存，导致挂载永久失败。
func downloadFile(filepath string, url string) error {
	c := &http.Client{
		Timeout:   2 * time.Minute,
		Transport: http.DefaultTransport,
	}
	// Get the data
	resp, err := c.Get(url)
	if err != nil {
		return fmt.Errorf("downloadFile: request to '%s' failed: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloadFile: unexpected http status %d (%s) for '%s'", resp.StatusCode, resp.Status, url)
	}

	tmpPath := filepath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("downloadFile: failed creating '%s': %w", tmpPath, err)
	}

	// Write the body to file
	written, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("downloadFile: failed writing '%s' after %d bytes: %w", tmpPath, written, copyErr)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("downloadFile: failed closing '%s': %w", tmpPath, closeErr)
	}
	if resp.ContentLength > 0 && written != resp.ContentLength {
		os.Remove(tmpPath)
		return fmt.Errorf("downloadFile: incomplete download of '%s', got %d bytes but expected %d", url, written, resp.ContentLength)
	}
	if err := os.Rename(tmpPath, filepath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("downloadFile: failed moving '%s' to '%s': %w", tmpPath, filepath, err)
	}

	golog.Info("download completed", "module", logModule, "url", url, "path", filepath, "bytes", written)
	return nil
}
