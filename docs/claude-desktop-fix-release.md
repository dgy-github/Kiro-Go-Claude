# Kiro-Go 1.1.3 Claude Desktop Stability Fix

This release is a local Claude Desktop / Claude Code gateway stability fix build.

Fork repository:
[dgy-github/Kiro-Go-Claude](https://github.com/dgy-github/Kiro-Go-Claude)

Upstream project:
[Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go)

## What This Fixes

- Prevents assistant turns from replaying fake tool transcripts such as `Tool results:` / `[Read]` instead of issuing real tool calls.
- Adds a continuation guard: when the user says `继续` / `可以` / `确定` after the assistant already planned a readonly inspection, Kiro-Go nudges the model to call real tools instead of restating the plan.
- Treats readonly diagnosis as pre-authorized in the backend prompt, so file reads, log reads, grep, and JSONL inspection should not require another confirmation.
- Adds a formal tool contract / repair loop. Claude, OpenAI Chat Completions, and Responses `tool_choice` requests are tracked inside the gateway; if upstream returns text-only placeholders when a tool is required, Kiro-Go runs one bounded repair retry instead of immediately passing the placeholder back to Claude Desktop.
- Keeps readonly file-check detection as a temporary fallback source for the tool contract when the client does not send `tool_choice`.
- Raises Kiro upstream streaming request timeout from 90 seconds to 5 minutes for large-context Claude Code sessions.
- Preserves image-bearing current `tool_result` payloads while keeping orphan text tool results flattened for upstream compatibility.
- Adds diagnostics for Kiro payload size, endpoint status, stream timing, and 400 payload-shape issues.
- Adds per-account in-flight request tracking. Concurrent requests now prefer idle accounts first, which reduces the chance of overloading one account and causing repeated 429 retry storms.

## Build

```powershell
cd D:\Kiro-Go
go test ./...
go build -o kiro-go.exe .
```

## Release Attachment

Upload `kiro-go.exe` as a GitHub Release asset. Do not commit it to git because `.gitignore` intentionally excludes `*.exe`.

Recommended release name:

```text
Kiro-Go 1.1.3 Claude Desktop Stability Fix
```

## Safety Notes

- Do not upload `D:\Kiro-Go\data\config.json`.
- Do not upload `D:\Kiro-Go\data\logs\`.
- Do not upload account tokens, refresh tokens, API keys, or local cache exports.
- `kiro-go.exe~` is only a local backup and should not be released.

## Windows Quick Start

Run:

```powershell
.\start-kiro-go-claude.bat
```

To create a desktop shortcut:

```powershell
powershell -ExecutionPolicy Bypass -File .\create-kiro-go-shortcut.ps1
```
