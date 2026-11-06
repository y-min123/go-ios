# CODEx_NOTES

本仓库是 go-ios 的定制分支，产物 `ios` / `ios.exe` / `libgoios.dylib` 被内嵌进
AnyLocation（macOS 客户端，仓库 `gitee/anyLocation`）与对应的 Windows 客户端。
面向 go-ios 自身开发规范见 `AGENTS.md`，本文只记录这个分支特有的坑与验收点。

## 当前状态

- 工作区有一组**未提交**的 VendDocuments 改动（`ios/house_arrest/house_arrest.go`、
  `cmd_device_files.go`、`main.go`），见下方「修改记录」。
- `AnyLocation/Source/ios` 已同步为本分支 `scripts/build_catalina_universal.sh` 的产物。
- 另有 `ios/imagemounter/*`、`cmd_device_debug.go` 的未提交改动（iOS 18 挂载镜像相关，
  非本次引入），构建产物会一并包含它们。

## 如何正确打包

照着这一节从头到尾走一遍即可，不需要额外背景知识。
**核心规则一句话：交付给客户端的二进制只能由 `scripts/build_catalina_universal.sh` 产出，
永远不要用裸 `go build`。**

### 步骤 0：一次性准备工具链

构建必须用 `go1.22.5`，且脚本只在两个固定位置找它，绝不回落到 PATH 上的 go：

- `~/go/bin/go1.22.5`（golang.org/dl 的 wrapper）
- `~/sdk/go1.22.5/bin/go`

任一存在即可。都没有就装一次：

```bash
go install golang.org/dl/go1.22.5@latest
go1.22.5 download
```

也可以显式指定：`GO_BIN=/path/to/go1.22.5 ./scripts/build_catalina_universal.sh`。

### 步骤 1：确认工作区内容就是你要发的

构建脚本编的是**当前工作区**，不是某个提交。未提交的改动会一起打进去。

```bash
git status --porcelain
```

逐条确认这些改动都是想要的。本仓库长期存在 `ios/imagemounter/*` 等未提交改动，
不确定就先问，别闷头编。

### 步骤 2：构建

```bash
cd /Users/hym/Documents/GitHub/Project/go-ios
./scripts/build_catalina_universal.sh
```

产物落在 `build/`：

| 文件 | 说明 |
|---|---|
| `build/ios` | **universal，这个才是交付物** |
| `build/libgoios.dylib` | universal 动态库 |
| `build/ios-x86_64-macos10.15` | 中间产物，min macOS 10.15 |
| `build/ios-arm64-macos11` | 中间产物，min macOS 11.0 |

脚本自带三项强制校验，任一不过直接失败退出：两个分片的 `minos` 分别是 10.15 / 11.0、
都不引用 macOS 12 才有的符号、都由 `go1.22.5` 构建。**看到 "Universal artifacts written to"
才算成功。**

`scripts/build_universal.sh` 是不带这些校验的旧版，**不要用它产出交付物**。

### 步骤 3：验收（脚本已经查过，但拷贝前再确认一遍）

```bash
B=build/ios
lipo -archs "$B"
for a in x86_64 arm64; do
  echo -n "$a minos: ";   otool -arch $a -l "$B" | grep -A3 LC_BUILD_VERSION | grep minos
  echo -n "$a macOS12: "; nm -arch $a -u "$B" | grep -c _SecTrustCopyCertificateChain
done
```

期望：`x86_64 arm64` / `minos 10.15` / `minos 11.0` / 两个计数都是 `0`。

如果本次改动新增了 CLI flag，再确认它真的被 docopt 识别（**不要 grep `--help`**，
那只是精简命令列表，不含 usage patterns）：

```bash
./build/ios <cmd> --yourflag ... --udid=DEADBEEF
# 识别 → 报 "Device not found"；未识别 → 吐 usage
```

### 步骤 4：拷到客户端仓库

```bash
cp build/ios /Users/hym/Documents/gitee/anyLocation/AnyLocation/Source/ios
chmod +x /Users/hym/Documents/gitee/anyLocation/AnyLocation/Source/ios
shasum -a 256 build/ios /Users/hym/Documents/gitee/anyLocation/AnyLocation/Source/ios
```

两个 sha256 必须一致。

### 步骤 5：客户端打包后再验一次

Xcode 的资源拷贝偶尔会因时间戳判断跳过，所以 `.app` 里那份要单独验：

```bash
B="<产物路径>/Fonelora Location Changer.app/Contents/Resources/ios"
# 重复步骤 3 的三条命令
```

对不上就在 Xcode 里 Clean Build Folder（⇧⌘K）后重编。

### Windows 产物

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o ios.exe .
```

主程序不需要 CGO（只有 `cmd/cdc-ncm` 需要）。Windows 没有 minos 那套约束，可以用裸
`go build`；但工具链版本建议与 macOS 侧保持一致，避免行为差异。要覆盖 ARM64 设备就再
编一份 `GOARCH=arm64`，Windows 不需要 lipo 合并。

### 常见失败与原因

| 现象 | 原因 |
|---|---|
| 脚本报 `must target macOS 10.15; got: 12.0` | 用了 PATH 上的新版 go，检查 `GO_BIN` |
| 脚本报 `still references macOS 12-only ...` | 同上，工具链没钉住 |
| 脚本报 `go1.22.5 not found` | 没装工具链，见步骤 0 |
| 二进制能跑但 10.15 用户打不开 | 用了裸 `go build` 或 `build_universal.sh`，重走步骤 2 |
| 新加的 flag 报 usage | 忘了在 `main.go` 的 usage 行加 `[--yourflag]` |
| 客户端行为没变 | `.app` 里还是旧二进制，见步骤 5 |

## 已知坑点

### 1. 裸 `go build` 会静默抬高部署目标

`go.mod` 的 `toolchain go1.22.5` 只是**下限不是上限**，而仓库根目录的 `go.work` 里写着
`toolchain go1.26.4`，所以在仓库内直接 `go build` 会被静默切到新版 Go，产出 `minos 12.0`
的二进制——在 macOS 10.15 / 11.x 上直接起不来。

`scripts/*.sh` 里的 `pinned_go()` 用 `GOWORK=off GOTOOLCHAIN=local` 就是为了压住这条。

### 2. macOS 12 专有符号是隐形的

新版 Go 的 `crypto/x509` 会引用 macOS 12 才引入的 `SecTrustCopyCertificateChain`。
**`minos` 和 `lipo -info` 都看不出这个问题**——二进制照常启动，只在走到系统证书链
校验时才因缺符号失败。所以验收必须单独查符号，见下方检查点。

### 3. `ios --help` 不是完整 usage

`ios --help` 打的是精简命令列表（约 112 行），**不含 docopt 的 usage patterns**。
想确认某个 flag 有没有被声明，别 grep `--help`，用这两种方式之一：

```bash
strings -a build/ios | grep -- "--yourflag"          # 看二进制
./ios <cmd> --yourflag ... --udid=DEADBEEF           # 识别→报设备找不到；未识别→吐 usage
```

### 4. 新增 flag 必须写进 `main.go` 的 usage

docopt 只解析出现在 usage pattern 里的参数。只改 `cmd_*.go` 读 `ctx.Args.Bool("--x")`
而不在 `main.go` 的 usage 行加 `[--x]`，运行时会被判为非法参数直接吐 usage。

### 5. `selfIdentity.plist` 会落在工作目录

`ios tunnel start` 会把主机身份（含 **Ed25519 私钥** + IRK）写到当前工作目录下的
`selfIdentity.plist`（`ios/tunnel/pairrecord.go:51`）。本仓库 `.gitignore` 已忽略它，
但**客户端仓库也要忽略**——客户端以自己的 `Source/` 作为子进程 `currentDirectoryURL`，
这个文件会生成在那里。绝不能提交、也不能打进安装包。

### 6. `house_arrest` 只有 `VendContainer`，对重签包无效

上游 `house_arrest.New` 只发 `VendContainer`，设备端对企业证书重签的包会拒绝并回
`InstallationLookupFailed`（错误名极具误导性，实际 App 装得好好的）。真正的门槛不是
`get-task-allow`，而是 `application-identifier` 与 bundle id 是否匹配。

要往任意签名的 App 推文件，用 `VendDocuments`（本分支新增的 `--documents`），
它只看 App Info.plist 的 `UIFileSharingEnabled`，与签名类型无关。
两种模式下 AFC 根都是容器根，`Documents/xxx` 路径前缀不变。

`ios/testmanagerd/xcuitestrunner.go` 也调 `house_arrest.New`，但 XCUITest runner 必然是
开发签名，那条路要保持 `VendContainer` 不变。

## 检查点

### 交付 macOS 二进制前

```bash
B=build/ios
lipo -archs "$B"                       # 期望：x86_64 arm64
for a in x86_64 arm64; do
  otool -arch $a -l "$B" | grep -A3 LC_BUILD_VERSION | grep minos
  nm -arch $a -u "$B" | grep -c _SecTrustCopyCertificateChain
done
```

期望：`x86_64 minos 10.15`、`arm64 minos 11.0`、两个符号计数均为 `0`。

### 客户端打包后再验一次

Xcode 产出的 `.app` 里那份也要验，防止拷贝阶段用了旧文件：

```bash
B="<App>.app/Contents/Resources/ios"
# 同上三条命令
```

### `--documents` 功能验收

需要两台不同签名状态的设备，或同一 App 的两个版本：

```bash
ios fsync --app=<bundleID> --documents tree --path=Documents --udid=<udid>
ios fsync --app=<bundleID>             tree --path=Documents --udid=<udid>
```

判定表（实测 5 个样本全部吻合）：

| App 状态 | `--documents` | 不带 |
|---|---|---|
| 有 `UIFileSharingEnabled` | 成功 | 取决于 app-id 是否匹配 |
| 无 `UIFileSharingEnabled` | `InstallationLookupFailed` | 同上 |

看 App 属性用 `ios apps --udid=<udid>`（不带 `--list`，输出 JSON），关注
`UIFileSharingEnabled`、`Entitlements.application-identifier`、`SignerIdentity`。

## 修改记录

### 2026-11-06 修复 userspace 隧道数据面断开后永不自愈

- 现象：iOS 17+ userspace 隧道改一会儿定位后，设备侧数据面 EOF 断开，`image auto` 拨
  `127.0.0.1:<userspaceTunPort>` 得到 `connection refused` → 客户端一直 `mounted image failed`。
- 根因：`userspace_tunnel.go` 的 teardown goroutine 虽在数据面死亡时关掉了监听（日志
  `data plane stopped unexpectedly`），但没把死隧道从 `TunnelManager.m.tunnels` 删除；
  `UpdateTunnels` 只在"设备消失 / connectionType 变化"时重建，同设备同通道直接 `continue`，
  于是死隧道永久滞留、`/tunnels` 仍上报它，永不重建。注释里"重建交给 TunnelManager"未兑现。
- 修法（事件置标志 + UpdateTunnels 识别重建，不往创建链塞 manager 引用）：
  - `ios/tunnel/tunnel.go`：`Tunnel` 加未导出字段 `dataPlaneDown *atomic.Bool` 和方法
    `isDataPlaneDown()`（nil 视为存活）。
  - `ios/tunnel/userspace_tunnel.go`：teardown goroutine 的 `!explicitClose` 分支置
    `dataPlaneDown.Store(true)`，返回的 `Tunnel` 带上该标志。
  - `ios/tunnel/tunnel_api.go`：`UpdateTunnels` 复用条件加 `&& !existing.isDataPlaneDown()`，
    死隧道走 stop+rebuild（日志 `rebuilding tunnel after data plane died`）。
  - 测试：`tunnel_api_test.go` 加 `TestTunnelIsDataPlaneDown`（纯函数三态）。重建路径因
    `startTunnel` 内先调真机 `ios.GetProductVersion` 无法无设备单测，与既有覆盖一致。
- 自愈延迟 ~1s（`main.go:1861` UpdateTunnels ticker 1s）。重建会按 `basePort+portOffset`
  取新端口（与 connectionType 切换同路径），客户端每次从 `/tunnels` 读当前端口，透明。
- 客户端侧 App（`gitee/anyLocation`）已于 2026-11-03 加 `isUserspaceTunnelDataPlaneAlive`
  走整进程重启兜底；本修复让 go-ios 自身也能恢复，两者不冲突。待办：重编二进制真机回归。

### 2026-09-10 新增 `fsync --documents`（VendDocuments）

- `ios/house_arrest/house_arrest.go`：`vendContainer` 泛化为 `vend(conn, bundleID, command)`；
  `New` 保持发 `VendContainer` 不变；新增 `NewDocuments` 发 `VendDocuments`。
- `cmd_device_files.go`：`runFsyncCommand` 读 `--documents`，为真走 `NewDocuments`。
- `main.go`：两条 fsync usage 行加 `[--documents]` 并补说明。
- 补丁副本：`gitee/anyLocation` 仓库的 `tests/go-ios-vend-documents.patch`（基于 `7fafca2`）。
- 起因：客户端推 `pairrecord.plist` / `host.p12` 到企业重签的 iOS App 时恒报
  `InstallationLookupFailed`。已在真实企业签名包（CITIC PACIFIC）上验证 push/pull 往返正常。
