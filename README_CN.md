# Kiro-Go

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

将 Kiro 账号转换为 OpenAI / Anthropic 兼容的 API 服务。

[English](README.md) | 中文

如果这个项目帮到了你，欢迎点个 Star 支持一下。

## 关于这个 Fork

这个 fork 基于 Kiro-Go，重点面向 Claude Desktop / Claude Code 作为网关使用的场景。
`v1.1.3-claude-fix` 版本主要修复长时间 coding-agent 会话里的稳定性问题：

- 过滤被污染的 assistant 历史，避免模型把假的 `Tool results:` / `[Read]` 文本当成真实工具结果反复复读。
- 用户已经说 `继续` / `可以` / `确定` / `直接做` 后，减少二次确认和只写计划不执行的问题。
- 将上游流式请求超时时间调高，缓解大上下文会话中途断流。
- 保留带图片的当前 `tool_result` 结构化内容，避免 Claude Desktop 丢上下文。
- 增加账号池 in-flight 并发占用跟踪，让并发请求优先选择空闲账号，降低同一个账号被打爆后触发 429 重试风暴的概率。

原始上游项目：[Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go)。

如果用于 Windows + Claude Desktop，建议直接下载 Release：
[Kiro-Go-Claude Releases](https://github.com/dgy-github/Kiro-Go-Claude/releases)。

## 功能特性

- Anthropic `/v1/messages` 与 OpenAI `/v1/chat/completions`
- 多账号池轮询负载均衡
- 自动 Token 刷新、SSE 流式输出、Web 管理面板
- 多种认证方式：AWS Builder ID、IAM Identity Center (企业 SSO)、SSO Token、本地缓存、凭证 JSON
- 用量追踪、账号导入导出、中英双语
- 支持设置出站代理（SOCKS5 / HTTP）

## 快速开始

### Docker Compose（推荐）

```bash
git clone https://github.com/dgy-github/Kiro-Go-Claude.git
cd Kiro-Go-Claude
mkdir -p data
docker-compose up -d
```

### Docker 运行

下面的镜像是上游镜像，可能不包含本 fork 的 Claude 稳定性修复。
如果要使用本 fork 的修复版，建议优先使用 Docker Compose 或源码编译，除非你已经发布了自己的镜像。

```bash
docker run -d \
  --name kiro-go \
  -p 8080:8080 \
  -e ADMIN_PASSWORD=your_secure_password \
  -v /path/to/data:/app/data \
  --restart unless-stopped \
  ghcr.io/quorinex/kiro-go:latest
```

### 源码编译

```bash
git clone https://github.com/dgy-github/Kiro-Go-Claude.git
cd Kiro-Go-Claude
go build -o kiro-go .
./kiro-go
```

### Windows Claude Desktop 构建版

从 Release 页面下载 `kiro-go-1.1.3-claude-fix-windows-amd64.zip`，解压后运行：

```powershell
.\start-kiro-go-claude.bat
```

发布压缩包不会包含 `data/config.json`、日志、账号 token 或本地凭证缓存文件。

### 部署到 Zeabur

仓库已包含 `Dockerfile`，可直接在 Zeabur 上构建运行。

**方式一：面板一键部署**

1. Fork 本仓库到你的 GitHub 账号。
2. 在 Zeabur 新建服务，选择 **Deploy from GitHub**，绑定刚才 fork 的仓库。
3. Zeabur 自动识别 `Dockerfile` 并完成构建。
4. 在 **Networking** 标签暴露端口 `8080` 并绑定域名。
5. 在 **Variables** 标签至少设置 `ADMIN_PASSWORD`（管理面板密码）。
6. 如需持久化账号 / 配置，挂载 Volume 到 `/app/data`。

**方式二：CLI 部署**

```bash
npm i -g zeabur
zeabur auth login
zeabur deploy
```

> 命令需在项目根目录执行。CLI 会生成 `.zeabur/context.json` 记录目标 project / service，包含个人 ID，请勿提交。

部署完成后访问 `https://<你的域名>/admin` 登录管理面板。

首次运行会在 `data/config.json` 自动生成配置，挂载 `/app/data` 以持久化。默认管理密码为 `changeme`，生产环境请务必通过 `ADMIN_PASSWORD` 环境变量或在管理面板中修改。

## 使用方法

访问 `http://localhost:8080/admin` 登录、添加账号，然后调用 API：

```bash
# Claude
curl http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-sonnet-4.5","max_tokens":1024,"messages":[{"role":"user","content":"你好！"}]}'

# OpenAI
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer any" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"你好！"}]}'
```

## 思考模式

在模型名后加后缀（默认 `-thinking`）即可启用，例如 `claude-sonnet-4.5-thinking`。Claude 兼容请求如果带有顶层 `thinking` 配置，例如 `{"type":"enabled","budget_tokens":2048}` 或 `{"type":"adaptive"}`，也会自动启用 thinking 模式。输出格式可在管理面板「设置 - Thinking 模式」中配置。

## 出站代理

可在管理面板「设置 - 出站代理设置」中配置代理。支持 SOCKS5 和 HTTP 代理。

设置保存后即时生效，无需重启服务。

## 环境变量

| 变量 | 说明 | 默认值 |
|-----|------|-------|
| `CONFIG_PATH` | 配置文件路径 | `data/config.json` |
| `ADMIN_PASSWORD` | 管理面板密码（覆盖配置文件） | - |

## 参与贡献

欢迎友好交流。遇到问题时，建议先让 Claude Code、Codex 等工具帮忙排查一下，大部分问题都能自己解决。如果能直接提个 PR 就更好了。

## 友情链接

- [LINUX DO](https://linux.do)

## 免责声明

本项目仅供学习和研究目的使用，与 Amazon、AWS 或 Kiro 没有任何关联。用户需自行确保使用行为符合所有适用的服务条款和法律法规，使用风险自负。

## 许可证

[MIT](LICENSE)
