
## Learned

- GSD on pi runtime: agent personas are NOT shipped by `npx @opengsd/gsd-core --pi --global` (programmatic dispatch by design); working set lives in `~/.pi/agent/agents/gsd/*.md` (frontmatter name/description + `tools: read, grep, find, ls, bash, edit, write`, paths `~/.claude/`→`~/.pi/agent/`). Missing personas = subagent spawn fails by name.
- Go toolchain: installed 1.25.12 at /usr/local/go (tarball); full-suite `-race` OOMs on the 2 SSA/RTA audit tests (11.9 GB RAM, no swap) — run them non-race with GOGC/GOMEMLIMIT tuning.
