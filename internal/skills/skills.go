// Package skills loads Pi-compatible Agent Skills from a directory.
//
// Skills are read once at startup. There is intentionally no runtime reload:
// a restart is the safety boundary for changed skill instructions.
package skills

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Skill is a loaded skill directory.
type Skill struct {
	Name                   string
	Description            string
	Path                   string
	File                   string
	Body                   string
	License                string
	Compatibility          string
	AllowedTools           []string
	DisableModelInvocation bool
	RequiresShell          bool
}

// Load discovers SKILL.md files under root. It returns valid skills sorted by
// name and a list of warnings for invalid skills. A missing root is not an
// error.
func Load(root string) ([]Skill, []string) {
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("skills: %v", err)}
	}
	if !info.IsDir() {
		return nil, []string{fmt.Sprintf("skills: %s is not a directory", root)}
	}

	var skills []Skill
	var warnings []string
	seen := make(map[string]bool)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skills: %v", err))
			return nil
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}
		skill, err := loadSkill(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skills: %s: %v", path, err))
			return nil
		}
		if seen[skill.Name] {
			warnings = append(warnings, fmt.Sprintf("skills: duplicate skill %q at %s", skill.Name, path))
			return nil
		}
		seen[skill.Name] = true
		skills = append(skills, skill)
		return nil
	})
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("skills: %v", err))
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, warnings
}

var namePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func loadSkill(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	content := strings.TrimPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\uFEFF")
	frontmatter, body, ok := splitFrontmatter(content)
	if !ok {
		return Skill{}, fmt.Errorf("missing frontmatter")
	}

	fields, listValues, err := parseFrontmatter(frontmatter)
	if err != nil {
		return Skill{}, err
	}

	name := strings.TrimSpace(fields["name"])
	description := strings.TrimSpace(fields["description"])
	if name == "" {
		return Skill{}, fmt.Errorf("missing name")
	}
	if !namePattern.MatchString(name) {
		return Skill{}, fmt.Errorf("invalid name %q", name)
	}
	if description == "" {
		return Skill{}, fmt.Errorf("missing description")
	}

	dir := filepath.Dir(path)
	skill := Skill{
		Name:                   name,
		Description:            description,
		Path:                   dir,
		File:                   path,
		Body:                   strings.TrimSpace(body),
		License:                strings.TrimSpace(fields["license"]),
		Compatibility:          strings.TrimSpace(fields["compatibility"]),
		AllowedTools:           listValues["allowed-tools"],
		DisableModelInvocation: parseBool(fields["disable-model-invocation"]),
	}
	skill.RequiresShell = requiresShell(skill, body)
	return skill, nil
}

func splitFrontmatter(content string) (frontmatter, body string, ok bool) {
	if !strings.HasPrefix(content, "---\n") {
		return "", "", false
	}
	rest := content[len("---\n"):]
	index := strings.Index(rest, "\n---")
	if index < 0 {
		return "", "", false
	}
	frontmatter = rest[:index]
	body = rest[index+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")
	return frontmatter, body, true
}

func parseFrontmatter(content string) (map[string]string, map[string][]string, error) {
	fields := make(map[string]string)
	lists := make(map[string][]string)
	lines := strings.Split(content, "\n")
	currentList := ""

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if currentList != "" && strings.HasPrefix(trimmed, "- ") {
				lists[currentList] = append(lists[currentList], unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))))
			}
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		currentList = ""
		if value == "" {
			if key == "allowed-tools" {
				currentList = key
			}
			continue
		}
		if key == "allowed-tools" {
			var inline []string
			if err := json.Unmarshal([]byte(value), &inline); err == nil {
				lists[key] = inline
			} else {
				lists[key] = splitCSV(value)
			}
			continue
		}
		fields[key] = unquote(value)
	}
	return fields, lists, nil
}

func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconvUnquote(value); err == nil {
			return unquoted
		}
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	return value
}

func strconvUnquote(value string) (string, error) {
	var decoded string
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return "", err
	}
	return decoded, nil
}

func splitCSV(value string) []string {
	value = strings.Trim(value, "[]")
	var result []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, unquote(part))
		}
	}
	return result
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "1":
		return true
	default:
		return false
	}
}

func requiresShell(skill Skill, body string) bool {
	for _, tool := range skill.AllowedTools {
		switch strings.ToLower(strings.TrimSpace(tool)) {
		case "bash", "shell", "sh", "exec", "execute", "terminal":
			return true
		}
	}
	if info, err := os.Stat(filepath.Join(skill.Path, "scripts")); err == nil && info.IsDir() {
		return true
	}
	lower := strings.ToLower(body)
	for _, marker := range []string{"```sh", "```bash", "bash ", "/bin/sh", "node scripts/", "python scripts/", "go run "} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// PromptSection renders the skills advertisement for the system prompt.
func PromptSection(all []Skill) string {
	var available []Skill
	for _, skill := range all {
		if !skill.DisableModelInvocation {
			available = append(available, skill)
		}
	}
	if len(available) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("<skills>\n")
	builder.WriteString("The following skills provide specialized instructions for specific tasks.\n")
	builder.WriteString("Use the read tool to load a skill's file when the task matches its description.\n")
	builder.WriteString("Resolve relative paths against the skill's directory.\n")
	builder.WriteString("In FOLIA mode, never execute shell commands or modify files. If a skill requires shell, explain that NORMAL or YOLO mode is required.\n\n")
	builder.WriteString("<available_skills>\n")
	for _, skill := range available {
		builder.WriteString("  <skill>\n")
		builder.WriteString("    <name>" + html.EscapeString(skill.Name) + "</name>\n")
		builder.WriteString("    <description>" + html.EscapeString(skill.Description) + "</description>\n")
		builder.WriteString("    <location>" + html.EscapeString(skill.File) + "</location>\n")
		if skill.RequiresShell {
			builder.WriteString("    <requires_shell>true</requires_shell>\n")
		}
		builder.WriteString("  </skill>\n")
	}
	builder.WriteString("</available_skills>\n</skills>")
	return builder.String()
}

// InvocationPrompt renders a skill for explicit /skill:<name> use.
func (s Skill) InvocationPrompt(args string) string {
	var builder strings.Builder
	builder.WriteString("Use the following skill.\n\n")
	builder.WriteString("<skill name=\"" + html.EscapeString(s.Name) + "\" location=\"" + html.EscapeString(s.Path) + "\">\n")
	builder.WriteString(s.Body)
	builder.WriteString("\n</skill>")
	if strings.TrimSpace(args) != "" {
		builder.WriteString("\n\nUser request: " + strings.TrimSpace(args))
	}
	return builder.String()
}
