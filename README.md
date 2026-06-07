# Kiro-Go

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Convert Kiro accounts to OpenAI / Anthropic compatible API service.

[English](README.md) | [中文](README_CN.md)

If this project helps you, a Star would mean a lot.

## About This Fork

This fork tracks Kiro-Go and focuses on a Claude Desktop / Claude Code gateway use case.
The `v1.1.3-claude-fix` release adds stability fixes for long coding-agent sessions:

- Filters polluted assistant history that replays fake `Tool results:` / `[Read]` text instead of making real tool calls.
- Reduces unnecessary second confirmations after the user has already said `continue`, `yes`, or `do it`.
- Raises upstream streaming timeout for large-context sessions.
- Preserves image-bearing `tool_result` payloads where Claude Desktop still needs structured context.
- Adds per-account in-flight request tracking so concurrent requests prefer idle accounts instead of overloading one account and triggering 429 retry storms.

Original upstream project: [Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go).

For the Windows build used with Claude Desktop, download the latest zip from:
[Kiro-Go-Claude Releases](https://github.com/dgy-github/Kiro-Go-Claude/releases).

## Features

- Anthropic `/v1/messages` & OpenAI `/v1/chat/completions`
- Multi-account pool with round-robin load balancing
- Auto token refresh, SSE streaming, Web admin panel
- Multiple auth: AWS Builder ID, IAM Identity Center (Enterprise SSO), SSO Token, local cache, credentials JSON
- Usage tracking, account import/export, i18n (CN / EN)
- Support configuring outbound proxy (SOCKS5 / HTTP)

## Quick Start

### Docker Compose (Recommended)

```bash
git clone https://github.com/dgy-github/Kiro-Go-Claude.git
cd Kiro-Go-Claude
mkdir -p data
docker-compose up -d
```

### Docker Run

The image below is the upstream image and may not include this fork's Claude stability fixes.
For this fork, prefer Docker Compose or build from source unless you publish your own image.

```bash
docker run -d \
  --name kiro-go \
  -p 8080:8080 \
  -e ADMIN_PASSWORD=your_secure_password \
  -v /path/to/data:/app/data \
  --restart unless-stopped \
  ghcr.io/quorinex/kiro-go:latest
```

### Build from Source

```bash
git clone https://github.com/dgy-github/Kiro-Go-Claude.git
cd Kiro-Go-Claude
go build -o kiro-go .
./kiro-go
```

### Windows Claude Desktop Build

Download `kiro-go-1.1.3-claude-fix-windows-amd64.zip` from the release page, unzip it, then run:

```powershell
.\start-kiro-go-claude.bat
```

The release zip intentionally does not include `data/config.json`, logs, account tokens, or local credential cache files.

### Deploy on Zeabur

The repo already includes a `Dockerfile`, so it builds and runs on Zeabur out of the box.

**Option 1: Dashboard (one-click)**

1. Fork this repo to your GitHub account.
2. In Zeabur, create a new service and choose **Deploy from GitHub**, then select your fork.
3. Zeabur auto-detects the `Dockerfile` and builds the image.
4. In the **Networking** tab, expose port `8080` and bind a domain.
5. In the **Variables** tab, set at least `ADMIN_PASSWORD` (admin panel password).
6. Mount a Volume at `/app/data` if you want accounts / config to survive redeploys.

**Option 2: CLI**

```bash
npm i -g zeabur
zeabur auth login
zeabur deploy
```

> Run the commands from the project root. The CLI writes `.zeabur/context.json` to remember the target project / service — it contains personal IDs, so don't commit it.

Once the service is up, open `https://<your-domain>/admin` to log in.

Config is auto-created at `data/config.json`. Mount `/app/data` for persistence. The default admin password is `changeme` — override it via the `ADMIN_PASSWORD` env var or change it in the admin panel before going to production.

## Usage

Open `http://localhost:8080/admin`, log in, add accounts, then call the API:

```bash
# Claude
curl http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-sonnet-4.5","max_tokens":1024,"messages":[{"role":"user","content":"Hello!"}]}'

# OpenAI
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer any" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello!"}]}'
```

## Thinking Mode

Append a suffix (default `-thinking`) to the model name, e.g. `claude-sonnet-4.5-thinking`. Claude-compatible requests that include a top-level `thinking` config such as `{"type":"enabled","budget_tokens":2048}` or `{"type":"adaptive"}` also enable thinking mode automatically. Configure output format in the admin panel under Settings - Thinking Mode.

## Outbound Proxy

For users in restricted network regions, configure an outbound proxy in the admin panel under **Settings - Outbound Proxy Settings**. Supports SOCKS5 and HTTP proxies.

The setting takes effect immediately without restarting.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CONFIG_PATH` | Config file path | `data/config.json` |
| `ADMIN_PASSWORD` | Admin panel password (overrides config) | - |

## Contributing

Friendly discussion is welcome. If you run into issues, try asking Claude Code, Codex, or similar tools for help first — most problems can be solved that way. PRs are even better.

## Friend Links

- [LINUX DO](https://linux.do)

## Disclaimer

For educational and research purposes only. Not affiliated with Amazon, AWS, or Kiro. Users are responsible for complying with applicable terms of service and laws. Use at your own risk.

## License

[MIT](LICENSE)
