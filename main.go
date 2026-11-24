// Command foliarz starts the Foliarz TUI AI agent.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sort"
	"time"
	_ "time/tzdata"

	tea "charm.land/bubbletea/v2"

	"github.com/mdukat/foliarz/internal/agent"
	"github.com/mdukat/foliarz/internal/buildinfo"
	"github.com/mdukat/foliarz/internal/config"
	"github.com/mdukat/foliarz/internal/llm"
	"github.com/mdukat/foliarz/internal/logging"
	"github.com/mdukat/foliarz/internal/paths"
	"github.com/mdukat/foliarz/internal/projectcontext"
	"github.com/mdukat/foliarz/internal/session"
	"github.com/mdukat/foliarz/internal/skills"
	"github.com/mdukat/foliarz/internal/tools"
	"github.com/mdukat/foliarz/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "foliarz: %v\n", err)
		os.Exit(1)
	}
}

func run() (returnErr error) {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}

	dirs, err := paths.Resolve()
	if err != nil {
		return err
	}
	logFile, err := logging.Init(dirs.LogFilePath())
	if err != nil {
		return err
	}
	defer logFile.Close()
	defer func() {
		if returnErr != nil {
			slog.Error("foliarz failed", "error", returnErr)
		}
	}()
	slog.Info("foliarz starting", "version", buildinfo.String(), "workDir", workDir)
	if removed := tools.CleanOutputFiles(7 * 24 * time.Hour); removed > 0 {
		slog.Info("pruned old bash output files", "count", removed)
	}

	cfg, err := config.Load(dirs)
	if err != nil {
		return err
	}
	provider, model, err := cfg.DefaultModel()
	if err != nil {
		return err
	}

	loadProjectContext := func(dir string) (string, error) {
		file, found, err := projectcontext.Load(dir)
		if err != nil {
			return "", err
		}
		if !found {
			return "", nil
		}
		return file.Prompt(), nil
	}
	projectContext, err := loadProjectContext(workDir)
	if err != nil {
		return err
	}

	compactionEnabled, compactionReserve, compactionKeep := cfg.TUI.Compaction.Resolve()
	compactionSettings := tui.CompactionSettings{
		Enabled:          compactionEnabled,
		ReserveTokens:    compactionReserve,
		KeepRecentTokens: compactionKeep,
	}

	thinkingLevel := cfg.TUI.ActiveThinkingLevel()

	var modelChoices []tui.ModelChoice
	providerIDs := make([]string, 0, len(cfg.Providers))
	for providerID := range cfg.Providers {
		providerIDs = append(providerIDs, providerID)
	}
	sort.Strings(providerIDs)
	for _, providerID := range providerIDs {
		configuredProvider := cfg.Providers[providerID]
		modelIDs := make([]string, 0, len(configuredProvider.Models))
		for modelID := range configuredProvider.Models {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)
		for _, modelID := range modelIDs {
			configuredModel := configuredProvider.Models[modelID]
			modelChoices = append(modelChoices, tui.ModelChoice{
				Provider:      providerID,
				ID:            modelID,
				Name:          configuredModel.Name,
				ContextWindow: configuredModel.ContextWindow,
			})
		}
	}

	// Skills are intentionally loaded only at startup. Reloading requires a
	// restart so changed skill instructions cannot enter a live session.
	loadedSkills, skillWarnings := skills.Load(dirs.SkillsDir())
	for _, warning := range skillWarnings {
		slog.Warn(warning)
	}
	if len(loadedSkills) > 0 {
		slog.Info("skills loaded", "count", len(loadedSkills))
	}

	allTools := tools.All()
	buildResponder := func(providerID, modelID string) (tui.Responder, []string, error) {
		selectedProvider, ok := cfg.Provider(providerID)
		if !ok {
			return nil, nil, fmt.Errorf("provider %q is not configured", providerID)
		}
		selectedModel, ok := selectedProvider.Models[modelID]
		if !ok {
			return nil, nil, fmt.Errorf("model %q is not configured for provider %q", modelID, providerID)
		}

		api := selectedModel.API
		if api == "" {
			api = selectedProvider.API
		}
		if api != "" && api != "openai-completions" {
			return nil, nil, fmt.Errorf("unsupported API %q for %s/%s", api, providerID, modelID)
		}

		baseURL := selectedModel.BaseURL
		if baseURL == "" {
			baseURL = selectedProvider.BaseURL
		}
		headers := make(map[string]string, len(selectedProvider.Headers)+len(selectedModel.Headers))
		for key, value := range selectedProvider.Headers {
			headers[key] = value
		}
		for key, value := range selectedModel.Headers {
			headers[key] = value
		}
		client := llm.New(baseURL, selectedProvider.APIKey, headers)
		responder := agent.New(client, selectedModel, cfg.SystemPrompt, allTools, loadedSkills,
			agent.WithCompaction(compactionSettings.Enabled, compactionSettings.ReserveTokens, compactionSettings.KeepRecentTokens),
		)
		return responder, selectedModel.ThinkingLevels(), nil
	}

	responder, thinkingLevels, err := buildResponder(provider.ID, model.ID)
	if err != nil {
		return err
	}

	activeProviderID := provider.ID
	activeModelID := model.ID
	saveSettings := func(providerID, modelID, level string) error {
		if err := config.SaveLastUsed(dirs, providerID, modelID, level); err != nil {
			return err
		}
		cfg.TUI.LastProvider = providerID
		cfg.TUI.LastModel = modelID
		cfg.TUI.LastThinkingLevel = level
		activeProviderID = providerID
		activeModelID = modelID
		thinkingLevel = level
		return nil
	}

	store := session.NewStore(dirs.SessionsDir())

	newSession := func(dir string) *session.Session {
		context, err := loadProjectContext(dir)
		if err != nil {
			slog.Warn("project context load failed", "workDir", dir, "error", err)
			context = ""
		}
		sess := session.New(dir)
		sess.Provider = activeProviderID
		sess.Model = activeModelID
		sess.ThinkingLevel = thinkingLevel
		if activeProvider, ok := cfg.Provider(activeProviderID); ok {
			if activeModel, ok := activeProvider.Models[activeModelID]; ok {
				sess.ContextWindow = activeModel.ContextWindow
				if levels := activeModel.ThinkingLevels(); len(levels) > 0 && !slices.Contains(levels, sess.ThinkingLevel) {
					sess.ThinkingLevel = levels[0]
					thinkingLevel = levels[0]
				}
			}
		}
		sess.ProjectContext = context
		return sess
	}
	sess := newSession(workDir)
	sess.ProjectContext = projectContext

	p := tea.NewProgram(tui.New(sess,
		tui.WithThinkingLevels(thinkingLevels),
		tui.WithNewSession(newSession),
		tui.WithResponder(responder),
		tui.WithResponderFactory(buildResponder),
		tui.WithSettingsChanged(saveSettings),
		tui.WithHideThinking(cfg.TUI.HideThinkingBlock),
		tui.WithSystemPrompt(cfg.SystemPrompt),
		tui.WithTools(allTools),
		tui.WithSkills(loadedSkills),
		tui.WithModels(modelChoices),
		tui.WithSessionStore(store),
		tui.WithProjectContextLoader(loadProjectContext),
		tui.WithCompaction(compactionSettings),
	))
	_, err = p.Run()
	return err
}
