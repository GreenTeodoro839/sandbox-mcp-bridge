# sandbox-mcp-bridge

运行在**手机上**的极简 MCP 服务器，给手机 AI 助手（如小米 Miclaw）补上"读写本机文件"的能力。它是 [sandbox-mcp](https://github.com/GreenTeodoro839/sandbox-mcp) 服务端的手机端搭档。

## 它解决什么

手机助手能调用远程沙箱（sandbox-mcp）跑命令、处理文件，但**手机本身没有 `curl` 之类工具**，没法把本机文件推到沙箱、或把结果存回本机。本桥接器以 KernelSU/Magisk 模块形式跑在手机上，监听 `127.0.0.1:8765`，只做一件事：在**本机文件**和 **URL** 之间搬字节。

典型流程：
1. 助手向沙箱要一个 `upload_url` → 调本桥 `push_file(本机路径, upload_url)` 把文件推进沙箱
2. 沙箱处理完给出 `download_url` → 调本桥 `pull_file(download_url, 本机路径)` 存回手机

## 工具

| 工具 | 作用 |
|---|---|
| `push_file(local_path, url)` | 把本机文件 PUT 到一个 URL（如沙箱 upload_url） |
| `pull_file(url, local_path)` | 把一个 URL GET 下来存到本机（如沙箱 download_url） |
| `list_files(dir)` | 列本机目录 |
| `read_text(path)` | 读本机小文本文件（≤200KB） |

> 它**没有 shell、不能执行命令**——命令/沙箱类任务走 sandbox-mcp 服务端，不是这里。

## 安装

1. 到 [Releases](https://github.com/GreenTeodoro839/sandbox-mcp-bridge/releases) 下载 `local-mcp-bridge.zip`。
2. 在 **KernelSU / Magisk 管理器 → 模块 → 从本地安装** 选这个 zip 刷入。
3. 安装界面会打印一行 **bridge token**，形如：
   ```
   Authorization: Bearer 1a2b3c...（64 位十六进制）
   ```
   记下它（也可稍后用 `su -c 'cat /data/adb/local_mcp_bridge/token'` 再看）。
4. 重启手机，桥接器会随开机启动，监听 `http://127.0.0.1:8765/mcp`。
5. 在 Miclaw 里**再加一个 URL 型 MCP 服务器**：
   - URL：`http://127.0.0.1:8765/mcp`
   - 请求头：`Authorization: Bearer <上面那个 token>`

## Token 是怎么来的

- 模块安装时（`customize.sh`）自动生成一个随机 token（两段内核 UUID 拼成 64 位十六进制，不依赖额外工具），存到 `/data/adb/local_mcp_bridge/token`（仅 root 可读，`0600`）。
- 该路径在**模块目录之外**，所以更新/重装模块**不会**丢 token。
- 开机脚本 `service.sh` 从该文件读出 token，以 `BRIDGE_TOKEN` 环境变量传给桥接器；若文件意外缺失会在开机时补生成，保证鉴权始终开启。
- 想换新 token：删掉该文件再重装模块（或重启）即可重新生成，记得同步更新 Miclaw 里的请求头。

## 为什么需要 token

Android 上**任何本地 App 都能访问 `127.0.0.1:8765`**，而本桥以 root 运行、能读写任意文件。没有 token 就等于把"任意文件读写"开放给机上所有应用。因此每个请求都必须带 `Authorization: Bearer <token>`，否则 401。

## 从源码构建

需要 Go 和 Android NDK（native 构建，使用系统 DNS 解析器和 CA store，无需内置证书/自搓 DNS）。

```bash
# 用 NDK 的 clang 作为 CGO 编译器，目标 arm64 / Android API 24
export CC=$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android24-clang
CGO_ENABLED=1 GOOS=android GOARCH=arm64 go build -ldflags="-s -w" -o local-bridge-android .

# 打成可刷入的模块 zip
python3 build_zip.py local-bridge-android local-mcp-bridge.zip
```

发布版由 GitHub Actions（`.github/workflows/release.yml`）自动构建：打个 `v*` tag 推上去，CI 会编译 android-arm64 二进制、拼好 zip 并作为 Release 附件发布。

## 相关

- 服务端：[sandbox-mcp](https://github.com/GreenTeodoro839/sandbox-mcp) —— Docker 沙箱、命令执行、大文件签名 URL。
