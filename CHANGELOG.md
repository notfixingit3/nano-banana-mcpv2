# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-06-08

### Added
- Created the v2 fork under `nano-banana-mcpv2`.
- Added dynamic model resolution to `generate_image`, `edit_image`, and `continue_editing` tools.
- Supported specifying model name via a new optional `model` tool parameter.
- Supported server-wide default model configuration using the `GEMINI_IMAGE_MODEL` environment variable.
- Configured default model fallback to the newer standard `gemini-3.1-flash-image` (replacing legacy `gemini-2.5-flash-image-preview`).
- Added a `prepare` script to `package.json` to support direct installation from GitHub via git URL.
- Implemented automated release workflow via GitHub Actions (`.github/workflows/release.yml`) for main releases and beta pre-releases.
