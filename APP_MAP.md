# APP_MAP — nano-banana-mcpv2

Developer reference for the codebase. Single source of truth for architecture, entry points, and data flow.

---

## What it is

A [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that exposes Google's Gemini and Imagen image generation APIs as MCP tools. Clients (Claude Desktop, Claude Code, Cursor, etc.) communicate over **stdin/stdout using JSON-RPC 2.0**.

- Language: **Go 1.22+**
- Binary: single statically-linked executable (~8.5 MB), zero runtime dependencies
- Transport: **stdio** (not HTTP)
- Protocol: **MCP 2024-11-05**

---

## File Layout

```
nano-banana-mcpv2/
├── main.go                        # Entire server — one file
├── go.mod                         # Module: github.com/notfixingit3/nano-banana-mcpv2
├── Dockerfile                     # Multi-stage, scratch-based image
├── .github/
│   ├── FUNDING.yml                # Sponsorship links (GitHub Sponsors + Buy Me a Coffee)
│   └── workflows/
│       └── release.yml            # Tag-triggered cross-compile + GitHub Release
├── assets/
│   ├── logo.png
│   └── sample_output.png
├── scripts/
│   └── test_generation.sh         # Manual smoke-test over stdio
├── APP_MAP.md                     # This file
├── CHANGELOG.md
├── README.md
└── TEST_REPORT.md
```

---

## Architecture

```
MCP Client (Claude, Cursor, etc.)
        │  stdin  (JSON-RPC requests, newline-delimited)
        ▼
  ┌─────────────────────────────────────────┐
  │              main()                     │
  │  bufio.Reader → processRequestLine()    │
  │             → handleRequest()           │
  │                                         │
  │  initialize     → sendResponse()        │
  │  tools/list     → getToolsList()        │
  │  tools/call     → handleToolCall()      │
  │         │                               │
  │         ├── configure_gemini_token      │
  │         ├── get_configuration_status    │
  │         ├── generate_image  ────────────┼──► Gemini :generateContent
  │         ├── generate_imagen ────────────┼──► Gemini :predict (Imagen)
  │         ├── edit_image      ────────────┼──► Gemini :generateContent
  │         ├── continue_editing            │    (uses lastImagePath global)
  │         └── get_last_image_info        │
  └─────────────────────────────────────────┘
        │  stdout (JSON-RPC responses, newline-delimited)
        ▼
  MCP Client
```

---

## Entry Points

| Invocation | Behaviour |
|---|---|
| `./nano-banana-mcpv2` | Start MCP server (reads stdin forever) |
| `./nano-banana-mcpv2 --setup` | Interactive CLI wizard: prompts for API key, validates it, saves globally |

---

## Key Functions in `main.go`

| Function | Purpose |
|---|---|
| `main()` | Parses `--setup` flag, initialises logger and context, runs stdin read loop |
| `processRequestLine(line)` | Trims, parses JSON-RPC envelope, calls `handleRequest` |
| `handleRequest(req)` | Routes on `req.Method`: `initialize`, `tools/list`, `tools/call` |
| `getToolsList()` | Returns the static `[]Tool` slice — **edit here to add/change tools** |
| `handleToolCall(id, name, args)` | Dispatches to per-tool handlers; loads API key first |
| `handleGenerateImage(...)` | Calls `generativelanguage.googleapis.com/:generateContent` |
| `handleGenerateImagen(...)` | Calls `generativelanguage.googleapis.com/:predict` (Imagen model) |
| `handleEditImage(...)` | Reads image(s) from disk, base64-encodes, calls `:generateContent` |
| `loadConfig()` | Returns `(apiKey, source)` — checks env → local file → global file |
| `saveConfig(key)` | Writes `~/.nano-banana-config.json` (0600) |
| `resolveModel(custom)` | Priority: tool arg → `GEMINI_IMAGE_MODEL` env → `gemini-3.1-flash-image` |
| `getImagesDirectory()` | Returns save path: `./generated_imgs/` or `~/nano-banana-images/` on system paths |
| `sendResponse(id, result)` | Marshals and writes a JSON-RPC success response to stdout |
| `sendError(id, code, msg, data)` | Marshals and writes a JSON-RPC error response to stdout |
| `sendToolError(id, msg, data)` | Sends a tool-level error as a successful RPC response with `isError: true` |
| `runSetupWizard()` | Interactive setup: reads key, validates against models endpoint, saves |

---

## Tool Inventory

All tools are defined in `getToolsList()` and dispatched in `handleToolCall()`.

### `configure_gemini_token`
Saves the API key to `~/.nano-banana-config.json`.
| Param | Type | Required |
|---|---|---|
| `apiKey` | string | ✅ |

### `generate_image`
Gemini native multimodal image generation (`gemini-3.1-flash-image` default).
| Param | Type | Required |
|---|---|---|
| `prompt` | string | ✅ |
| `model` | string | — |
| `aspectRatio` | `1:1` `16:9` `9:16` `4:3` `3:4` | — |

### `generate_imagen`
Dedicated Imagen pipeline (`imagen-4.0-generate-001` default).
| Param | Type | Required |
|---|---|---|
| `prompt` | string | ✅ |
| `model` | `imagen-4.0-generate-001` `…-ultra-…` `…-fast-…` | — |
| `aspectRatio` | `1:1` `3:4` `4:3` `9:16` `16:9` | — |
| `numberOfImages` | int 1–4 | — |
| `negativePrompt` | string | — |

### `edit_image`
Reads an image from disk, sends it with the prompt to Gemini for editing.
| Param | Type | Required |
|---|---|---|
| `imagePath` | string (full path) | ✅ |
| `prompt` | string | ✅ |
| `referenceImages` | `[]string` (file paths) | — |
| `model` | string | — |
| `aspectRatio` | `1:1` `16:9` `9:16` `4:3` `3:4` | — |

### `continue_editing`
Same as `edit_image` but automatically uses `lastImagePath` (session global).
| Param | Type | Required |
|---|---|---|
| `prompt` | string | ✅ |
| `referenceImages` | `[]string` | — |
| `model` | string | — |

### `get_configuration_status`
Returns whether an API key is configured and where it came from.
No parameters.

### `get_last_image_info`
Returns path, size, and modified time of the last saved image in this session.
No parameters.

---

## Configuration Priority

```
1. Tool argument (model param)         ← highest
2. GEMINI_API_KEY / GEMINI_IMAGE_MODEL env vars
3. ~/.nano-banana-config.json          (written by configure_gemini_token / --setup)
4. ./nano-banana-config.json           (v1 migration — auto-promoted to global on first load)
```

Config file schema:
```json
{ "geminiApiKey": "your-key-here" }
```

---

## API Endpoints Used

| Tool | Endpoint |
|---|---|
| `generate_image`, `edit_image`, `continue_editing` | `POST https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent?key={key}` |
| `generate_imagen` | `POST https://generativelanguage.googleapis.com/v1beta/models/{model}:predict?key={key}` |
| `--setup` validation | `GET https://generativelanguage.googleapis.com/v1beta/models?key={key}` |

Auth: API key in query string. OAuth is not supported.

---

## Image Output

Saved automatically after every successful generation or edit.

| Platform | Path |
|---|---|
| Windows | `%USERPROFILE%\Documents\nano-banana-images\` |
| macOS/Linux (normal cwd) | `{cwd}/generated_imgs/` |
| macOS/Linux (system path) | `~/nano-banana-images/` |

Filename pattern:
- `generated-YYYYMMDD-HHMMSS-NNNNNN.png` — from `generate_image`
- `imagen-YYYYMMDD-HHMMSS-NNNNNN.png` — from `generate_imagen`
- `edited-YYYYMMDD-HHMMSS-NNNNNN.png` — from `edit_image` / `continue_editing`

The path of the most recently saved image is stored in the `lastImagePath` global and used by `continue_editing`.

---

## Environment Variables

| Variable | Effect |
|---|---|
| `GEMINI_API_KEY` | API key (overrides config file) |
| `GEMINI_IMAGE_MODEL` | Default model for `generate_image` / `edit_image` / `continue_editing` |
| `NANO_BANANA_LOG_FILE` | Path to diagnostic log file (safe — does not touch stdio) |

---

## Build & Release

```bash
# Local build
go build -o nano-banana-mcpv2 main.go

# Vet before building (also runs in CI)
go vet ./...

# Security scan
gosec ./...
```

Release is fully automated: push a `v*` tag on `dev`, the workflow cross-compiles 5 binaries and creates a GitHub Release. Tags containing `-` (e.g. `v0.1.2-beta.1`) are marked pre-release automatically.

Release targets: `linux/amd64`, `linux/arm64`, `darwin/arm64`, `darwin/amd64`, `windows/amd64`.

---

## Logging

All diagnostic output goes to the log file (`NANO_BANANA_LOG_FILE`), never to stderr or stdout, to keep the stdio MCP stream clean. If no log file is set, the server runs silently.

Fatal errors (stdin read failure) write to stderr and exit 1.

---

## Global State

| Variable | Type | Purpose |
|---|---|---|
| `lastImagePath` | `string` | Path of the most recently saved image; used by `continue_editing` |
| `httpClient` | `*http.Client` | Shared HTTP client, 60s timeout |
| `logFile` | `*os.File` | Log file handle (nil if logging disabled) |
| `globalCtx` | `context.Context` | Cancelled on SIGINT/SIGTERM; passed to all outbound HTTP requests |
