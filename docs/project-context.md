# Project instructions

If the session working directory contains an `AGENTS.md` file, Foliarz loads it
when a session starts and adds it to the model context as project instructions:

```xml
<project_instructions path="/home/user/project/AGENTS.md">
...file contents...
</project_instructions>
```

The instructions are appended to the system prompt sent with every request, so
the model follows them for the whole session. The startup info shows the system
prompt together with the project instructions.

`/new` starts a new session and reloads `AGENTS.md`, so edits made during a
session apply to the next one. Project instructions are not written to session
files; they are loaded from the working directory each time.

Only `AGENTS.md` in the session working directory (where Foliarz was started) is
loaded. Foliarz does not search parent directories or `~/.config/foliarz` for
context files.

If `AGENTS.md` exists but cannot be read at startup, Foliarz reports the error
and exits. An empty file is ignored.
