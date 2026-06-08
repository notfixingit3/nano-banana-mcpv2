# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.1-beta.0] - 2026-06-08

### Added
- Added release, build, and license badges to the `README.md`.
- Implemented global configuration file path (`~/.nano-banana-config.json`) as a fallback, solving path resolution issues when installing globally or running in different workspace directories.
- Added a new `generate_imagen` tool to utilize Google's dedicated Imagen generation model (`imagen-3.0-generate-002`) supporting multiple images, aspect ratios, and negative prompts.
- Added `aspectRatio` parameter to `generate_image` tool using the new `imageConfig` API in Gemini.
- Added transparent auto-migration of local configurations: loading a local `.nano-banana-config.json` automatically saves it globally to `~/.nano-banana-config.json` if no global file exists.
- Updated server initialization to dynamically read package metadata (`name` and `version`) from `package.json`, ensuring client lists display `"nano-banana-mcpv2"` and the correct pre-release/stable version.

## [0.1.0] - 2026-06-08

### Added
- Created the v2 fork under `nano-banana-mcpv2`.
- Added dynamic model resolution to `generate_image`, `edit_image`, and `continue_editing` tools.
- Supported specifying model name via a new optional `model` tool parameter.
- Supported server-wide default model configuration using the `GEMINI_IMAGE_MODEL` environment variable.
- Configured default model fallback to the newer standard `gemini-3.1-flash-image` (replacing legacy `gemini-2.5-flash-image-preview`).
- Added a `prepare` script to `package.json` to support direct installation from GitHub via git URL.
- Implemented automated release workflow via GitHub Actions (`.github/workflows/release.yml`) for main releases and beta pre-releases.
