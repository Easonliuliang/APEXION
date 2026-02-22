# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Cyber-Egyptian themed TUI (pyramid logo, gold/cyan color scheme)
- Pyramid-style spinner animation
- Doom loop detection (automatic recovery from repetitive agent behavior)
- Background bash execution mode
- Compact tool call display
- MCP (Model Context Protocol) support
- Session persistence with SQLite
- 17 built-in tools with permission levels
- Multi-provider support (Anthropic, OpenAI, DeepSeek, Gemini, and more)
- Custom slash commands via project config
- Non-interactive run mode for scripting

### Changed
- Renamed project from aictl to Apexion
- Input prompt changed to triangle symbol
- Status bar uses bold separator line

### Fixed
- Ctrl+C now properly exits when agent is running
