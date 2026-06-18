# ctxdump

`ctxdump` is a CLI tool to find, search, and dump previous local AI assistant conversations. 

It currently supports reading local history files from:
- **Codex CLI** (`~/.codex/sessions`, `$CODEX_HOME/sessions`)
- **Claude Code** (`~/.claude/conversations`, `~/.claude/`)
- **Gemini CLI** (`~/.gemini/sessions`, `~/.config/gemini/sessions`)
- **Antigravity IDE & Terminal Agents** (`~/.gemini/antigravity-ide/brain`, `~/.gemini/antigravity/brain`)

**Status**: V1 - This version discovers known local history files, lists them with an interactive TUI, and dumps their contents using agent-optimized formatting. It is designed primarily for other LLMs or coding agents that need to recover context from previous developer conversations.

## Installation

To install `ctxdump`, make sure you have Go installed, then run:

```bash
go install github.com/user/ctxdump/cmd/ctxdump@latest
```

## ⚠️ Safety Warning

**Conversations dumped by this tool may contain sensitive information**, including:
- Secrets, passwords, API keys, and credentials
- Output of executed commands
- Source code, proprietary logic, or private data
- Prompts and system instructions

The `ctxdump` tool is strictly read-only. It will never modify, delete, compact, or rewrite your source history files. However, if you are sharing or piping the output, please ensure no sensitive information is leaked.

## Usage

If you run `ctxdump` or `ctxdump search` without arguments in an interactive terminal, it will launch a rich Terminal User Interface (TUI) to help you find and copy your past conversations.

### Commands

* `ctxdump list`: Discover and list conversations.
* `ctxdump search <query>`: Search conversation contents and titles.
* `ctxdump dump <id-or-file>`: Dump a specific conversation.
* `ctxdump resume <provider> [id]`: Resume a conversation in its provider's native editor.

### Examples

Launch the interactive list:
```bash
ctxdump
```

List conversations sorted by date (instead of proximity to your current working directory):
```bash
ctxdump list --sort date
```

Search for a specific error or keyword across all your past AI conversations:
```bash
ctxdump search "compile error"
```

Dump the raw content of a conversation into a file:
```bash
ctxdump dump <id-or-file> --output markdown > transcript.md
```

Dump a conversation, including hidden thoughts and timestamps:
```bash
ctxdump dump <id-or-file> --include-thoughts --timestamps
```

Resume a previous conversation using its native CLI:
```bash
# Opens an interactive menu to select a Codex session, then runs `codex <id>`
ctxdump resume codex

# Resumes a specific Claude session using a custom execution template
ctxdump resume claude <id> --exec "claude --resume {path}"
```

## Future Scope

Planned features for future iterations:
* Better provider-specific parsing schemas
* Maximum token/character bounding
* Optional redaction of API keys and secrets
* MCP server mode
