# Skills

Skills give Foliarz specialized instructions and supporting files for a
particular kind of work. They follow the Pi Agent / Agent Skills layout: a
directory containing a `SKILL.md` file and any bundled scripts, references or
assets.

Foliarz loads skills from:

```
~/.config/foliarz/skills/
```

and discovers `SKILL.md` files recursively.

## SKILL.md

```markdown
---
name: web-search
description: Search the web through DuckDuckGo Lite. Use when you need current information or documentation.
license: MIT
compatibility: Requires Node.js 18+ and public internet access.
---

# Web search

Run the bundled script relative to this skill directory.
```

Supported frontmatter fields include `name`, `description`, `license`,
`compatibility`, `allowed-tools` and `disable-model-invocation`. Invalid skills
are skipped and logged as warnings in `foliarz.log`; duplicate names keep the
first skill discovered.

Skills are sorted by name.

## How skills are used

At startup Foliarz adds each enabled skill's name, description and absolute
`SKILL.md` path to the system prompt. The full instructions are not loaded until
the model reads `SKILL.md` with the `read` tool, or until the user explicitly
invokes a skill.

The startup screen lists all loaded skills. Skills that appear to need a shell
are marked `[shell]`; manually-invoked-only skills are marked `[manual]`.

Skills are loaded only at startup. There is intentionally no runtime reload:
restarting Foliarz is the safety boundary for changed skill contents.

### Explicit invocation

```
/skill:<name> [request]
```

For example:

```
/skill:web-search Go release notes
```

This loads the skill's `SKILL.md` instructions into the session as a user
request and starts a response. Relative paths in the skill resolve against its
directory.

## FOLIA safety

Skills cannot override the mode safety rules:

- In FOLIA, only read-only tools are available. The model can read a skill's
  instructions but cannot execute its scripts or modify files.
- Foliarz detects skills with `scripts/` directories, shell entries in
  `allowed-tools`, or shell commands in their instructions. Explicitly invoking
  such a skill in FOLIA fails before the model runs:
  `skill <name> requires shell and cannot run in FOLIA mode; switch to NORMAL or
  YOLO with /normal or /yolo`.
- In NORMAL, shell commands from skills still require the normal y/n approval.
  YOLO executes them without asking.
- The system prompt also states that in FOLIA mode the model must not execute
  shell commands or modify files, even when a skill asks it to.

## Copying Pi skills

Pi skills are compatible. To copy the skills installed under
`~/.pi/agent/skills/` into Foliarz:

```sh
mkdir -p ~/.config/foliarz/skills
cp -a ~/.pi/agent/skills/. ~/.config/foliarz/skills/
```

Restart Foliarz afterwards. Skills are loaded only at startup by design: this
keeps changed or untrusted skill instructions from entering an already-running
session without an explicit restart. The copied Pi skills
(`alpine-packages`, `debian-packages`, `get-proxy-address`, `go-packages`,
`printable-pdf`, `sejm-eli`, `web-search`, `web-search-proxy`,
`web-search-searxng`) appear at startup and can be invoked with `/skill:<name>`.
