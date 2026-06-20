# sandbox-mcp-bridge

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![release](https://img.shields.io/github/v/release/GreenTeodoro839/sandbox-mcp-bridge)](https://github.com/GreenTeodoro839/sandbox-mcp-bridge/releases/latest)
[![build](https://github.com/GreenTeodoro839/sandbox-mcp-bridge/actions/workflows/release.yml/badge.svg)](https://github.com/GreenTeodoro839/sandbox-mcp-bridge/actions/workflows/release.yml)

运行在**手机上**的极简 MCP **网关**：它是手机 AI 助手（如小米 Miclaw）唯一连接的那个 MCP，把远程 [sandbox-mcp](https://github.com/GreenTeodoro839/sandbox-mcp) 的沙箱工具**代理**过来，同时在本地提供**手机⇄沙箱的文件传输**——所有工具汇成一张表，避免被助手的"按查询选工具/选 server"路由拆散。

> 🖥️ **服务端**：[sandbox-mcp](https://github.com/GreenTeodoro839/sandbox-mcp) —— Docker 沙箱、命令执行、后台任务。网关在后端不可达时让连接失败，不把半残的 MCP 加载进上下文。

## 它解决什么

手机助手默认按查询语义只挑**一个** MCP/一批工具喂给模型，所以"沙箱"那轮里、另一个只管文件的 MCP 的工具根本不在候选集，助手就会说"做不到"。本网关把两边合成**一个 server**：

- **沙箱工具**（`exec` / `run_background` / `get_job` / `list_files` …）原样代理到你的远程 sandbox-mcp；
- **文件传输**（`upload_file` / `download_file`）在网关本地**改写**：后端那两个同名工具是"产签名 URL"（通用客户端自己 PUT/GET），网关把它们替换成"按**路径**直传/直存"——读手机文件流给沙箱、或把沙箱文件写回手机，字节不经过模型（避免 base64 截断）。

握手时网关会真的去 `initialize` 一次后端：通了才返回成功（并把后端的使用说明**原样**透传给模型——说明里只按名字提 `upload_file`/`download_file`，不提机制，所以两侧通用），不通就返回错误让 Miclaw 直接连接失败。

## 工具

网关把后端的 `upload_file` / `download_file`（URL 模式）**同名替换**成手机版（路径模式），其余沙箱工具原样代理：

| 工具 | 作用 |
|---|---|
| `upload_file(local_path, sandbox, dest)` | 把手机文件**一步**传进指定沙箱的 workspace（按路径，无需 URL） |
| `download_file(sandbox, src, local_path?)` | 给了 `local_path` 就把沙箱文件**存回手机**；不给就返回一个一次性下载直链给用户 |

## 安装

1. 到 [Releases](https://github.com/GreenTeodoro839/sandbox-mcp-bridge/releases) 下载 `local-mcp-bridge.zip`。
2. 在 **KernelSU / Magisk 管理器 → 模块 → 从本地安装** 选这个 zip 刷入。
3. 安装界面会打印一行 **bridge token**，形如：
   ```
   Authorization: Bearer 1a2b3c...（64 位十六进制）
   ```
   记下它（也可稍后用 `su -c 'cat /data/adb/modules/local_mcp_bridge/token'` 再看）。
4. **配置后端**（关键）：编辑 `sandbox.conf`，填你的远程 sandbox-mcp 地址和 token，然后重启：
   ```
   su -c 'vi /data/adb/modules/local_mcp_bridge/sandbox.conf'
   # SANDBOX_BASE_URL=https://你的服务器:端口      （PUBLIC_BASE_URL，不含末尾 /mcp）
   # SANDBOX_TOKEN=你的 SMCP_TOKEN                  （和服务端 MCP 用的同一个 Bearer）
   ```
   两项都填好前，Miclaw 的连接会（故意）加载失败。
5. 重启手机，网关随开机启动，监听 `http://127.0.0.1:8765/mcp`。
6. 在 Miclaw 里**只加这一个** URL 型 MCP 服务器（不要再单独加远程 sandbox-mcp，否则工具重复、又会被路由拆开）。把 `<BRIDGE_TOKEN>` 换成第 3 步那个 token：

   ```json
   {
     "mcpServers": {
       "sandbox": {
         "url": "http://127.0.0.1:8765/mcp",
         "headers": {
           "Authorization": "Bearer <BRIDGE_TOKEN>"
         }
       }
     }
   }
   ```

   > 沙箱的 `exec`/`run_background`/`upload_file`/`download_file` 等都会从这一个 server 里出现。

> 💡 改了 `sandbox.conf` 或想重连后端时，**不必重启手机**：在 KernelSU 管理器里点本模块的 **运行（Action）** 按钮即可快速重启网关，并打印运行状态。

## Token 是怎么来的

- 模块安装时（`customize.sh`）自动生成一个随机 token（两段内核 UUID 拼成 64 位十六进制，不依赖额外工具），存到**模块目录内** `/data/adb/modules/local_mcp_bridge/token`（仅 root 可读，`0600`）。
- 放在模块目录内的好处：**卸载模块时会随模块目录一起删除，不留残留**。
- 更新模块时模块目录会被新版替换，但 `customize.sh` 会把旧版的 token 拷贝过来，所以**更新后 token 不变**，无需重新粘贴到 Miclaw。
- 开机脚本 `service.sh` 从该文件读出 token，以 `BRIDGE_TOKEN` 环境变量传给桥接器；若文件意外缺失会在开机时补生成，保证鉴权始终开启。
- 想换新 token：删掉该文件再重启即可重新生成，记得同步更新 Miclaw 里的请求头。

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
