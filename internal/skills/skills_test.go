package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, dir, frontmatter, body string) string {
	t.Helper()
	path := filepath.Join(root, dir)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "read-only", "name: read-only\ndescription: Read-only skill.", "# Instructions\nDo something.")
	shellPath := writeSkill(t, root, "shell-skill", "name: shell-skill\ndescription: Uses a script.", "# Instructions\nRun the script.")
	if err := os.MkdirAll(filepath.Join(shellPath, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "nested/hidden", "name: hidden\ndescription: Hidden skill.", "body")

	loaded, warnings := Load(root)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded %d skills, want 3", len(loaded))
	}

	byName := make(map[string]Skill)
	for _, skill := range loaded {
		byName[skill.Name] = skill
	}
	if byName["read-only"].RequiresShell {
		t.Error("read-only skill marked as requiring shell")
	}
	if !byName["shell-skill"].RequiresShell {
		t.Error("shell skill not marked as requiring shell")
	}
	if byName["hidden"].Path == "" {
		t.Error("nested skill not loaded")
	}
}

func TestPromptSectionAndInvocation(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "web", "name: web\ndescription: Search the web.", "# Web skill\nRun a search.")
	writeSkill(t, root, "manual", "name: manual\ndescription: Manual only.\ndisable-model-invocation: true", "manual body")
	loaded, warnings := Load(root)
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}

	section := PromptSection(loaded)
	if strings.Contains(section, "<name>manual</name>") {
		t.Error("disable-model-invocation skill was advertised")
	}
	if !strings.Contains(section, "<name>web</name>") {
		t.Error("web skill is not advertised")
	}
	if !strings.Contains(section, "In FOLIA mode, never execute shell commands") {
		t.Error("skills prompt lacks the FOLIA safety rule")
	}

	web := loaded[0]
	if web.Name == "manual" {
		// Skills are sorted: manual, web.
		web = loaded[1]
	}
	prompt := web.InvocationPrompt("find Go docs")
	if !strings.Contains(prompt, "# Web skill") || !strings.Contains(prompt, "User request: find Go docs") {
		t.Errorf("InvocationPrompt() = %q", prompt)
	}
}

func TestInvalidSkillsAreWarnings(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "no-description", "name: no-description", "body")
	writeSkill(t, root, "bad-name", "name: Bad_Name\ndescription: x", "body")
	writeSkill(t, root, "first", "name: duplicate\ndescription: first", "body")
	writeSkill(t, root, "second", "name: duplicate\ndescription: second", "body")

	loaded, warnings := Load(root)
	if len(loaded) != 1 || loaded[0].Name != "duplicate" {
		t.Errorf("loaded = %+v", loaded)
	}
	if len(warnings) < 3 {
		t.Errorf("warnings = %v, want at least 3", warnings)
	}
}

func TestAllowedToolsShellDetection(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "tool", "name: tool\ndescription: tool\nallowed-tools: [bash, read]", "body")
	writeSkill(t, root, "safe", "name: safe\ndescription: safe\nallowed-tools: [read]", "body")
	loaded, _ := Load(root)
	for _, skill := range loaded {
		switch skill.Name {
		case "tool":
			if !skill.RequiresShell {
				t.Error("bash in allowed-tools was not detected")
			}
		case "safe":
			if skill.RequiresShell {
				t.Error("read-only allowed-tools marked shell")
			}
		}
	}
}
