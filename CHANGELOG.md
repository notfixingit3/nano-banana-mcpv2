# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.1-beta.0] - 2026-06-08

### Changed
- **Complete Go Rewrite**: Rewrote the entire MCP server in standard Go (1.22+), replacing the previous TypeScript/Node.js implementation entirely. This yields a single, statically linked ~8.5MB executable with zero external runtime dependencies.
- **Imagen Model Upgrade**: Updated the default Imagen model from the deprecated/404 `imagen-3.0-generate-002` to `imagen-4.0-generate-001` (Imagen 4).
- **Statically Defined Metadata**: Removed `package.json` dependency; server name and version are now compiled directly into the binary.

### Added
- **Robust File Logging**: Added diagnostic file logging via `NANO_BANANA_LOG_FILE` environment variable to safely trace RPC messages and API client status without corrupting the standard I/O stream.
- **Multi-model Imagen 4 Support**: Added support and autocomplete options for high-fidelity (`imagen-4.0-ultra-generate-001`) and speed-optimized (`imagen-4.0-fast-generate-001`) Imagen models.
- **Graceful Shutdown**: Implemented OS signal traps (`SIGINT`/`SIGTERM`) and propagated Go context controls to all outgoing API HTTP requests for quick cleanup.
- **Interactive Setup CLI Helper**: Added a `--setup` startup flag that validates the user's API key directly against Google's models list endpoint and saves it globally.
- Added release, build, and license badges to the `README.md`.
- Implemented global configuration file path (`~/.nano-banana-config.json`) as a fallback in Go, resolving path resolution issues when run globally.
- Added a new `generate_imagen` tool supporting multiple images, aspect ratios, and negative prompts.
- Added `aspectRatio` parameter to `generate_image` tool using the `imageConfig` API in Gemini.
- Added transparent auto-migration of local configurations from v1: loading a local `.nano-banana-config.json` automatically persists it globally.
- Added a multi-stage `Dockerfile` and automated cross-compiling release workflows for Go via GitHub Actions.

## [0.1.0] - 2026-06-08

### Added
- Created the v2 fork under `nano-banana-mcpv2`.
- Added dynamic model resolution to `generate_image`, `edit_image`, and `continue_editing` tools.
- Supported specifying model name via a new optional `model` tool parameter.
- Supported server-wide default model configuration using the `GEMINI_IMAGE_MODEL` environment variable.
- Configured default model fallback to the newer standard `gemini-3.1-flash-image` (replacing legacy `gemini-2.5-flash-image-preview`).
- Added a `prepare` script to `package.json` to support direct installation from GitHub via git URL.
- Implemented automated release workflow via GitHub Actions (`.github/workflows/release.yml`) for main releases and beta pre-releases.
