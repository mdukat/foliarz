package tui

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/mdukat/foliarz/internal/agent"
)

// shouldCompact reports whether the session context crossed the compaction
// threshold.
func (m Model) shouldCompact() bool {
	if m.compactor == nil || !m.compaction.Enabled || m.session.ContextWindow <= 0 {
		return false
	}
	threshold := m.session.ContextWindow - m.compaction.ReserveTokens
	if threshold < 1 {
		threshold = 1
	}
	return m.session.TokensUsed > threshold
}

// maybeCompact compacts the context when it is over the configured threshold.
func (m *Model) maybeCompact() {
	if !m.shouldCompact() {
		return
	}
	if err := m.compactContext(""); err != nil {
		slog.Error("context compaction failed", "session", m.session.ID, "error", err)
		m.lastErr = "compact context: " + err.Error()
		m.invalidateChat()
	}
}

// compactContext summarizes old context and updates the session boundary.
func (m *Model) compactContext(instructions string) error {
	if m.compactor == nil {
		return fmt.Errorf("compaction is not available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = agent.WithSessionID(ctx, m.session.ID)

	result, err := m.compactor.Compact(ctx, agent.CompactionInput{
		History:          slices.Clone(m.session.Messages),
		Summary:          m.session.Summary,
		ReadFiles:        slices.Clone(m.session.SummaryReadFiles),
		ModifiedFiles:    slices.Clone(m.session.SummaryModifiedFiles),
		FirstKept:        m.session.FirstKeptMessage,
		KeepRecentTokens: m.compaction.KeepRecentTokens,
		ThinkingLevel:    m.session.ThinkingLevel,
		Instructions:     instructions,
	})
	if err != nil {
		return err
	}

	m.session.Summary = result.Summary
	m.session.FirstKeptMessage = result.FirstKeptMessage
	m.session.SummaryReadFiles = result.ReadFiles
	m.session.SummaryModifiedFiles = result.ModifiedFiles
	m.session.SummaryTokensBefore = result.TokensBefore
	m.session.TokensUsed = result.EstimatedTokensAfter
	m.session.CostUSD += result.CostUSD
	slog.Info("context compacted",
		"session", m.session.ID,
		"firstKeptMessage", result.FirstKeptMessage,
		"tokensBefore", result.TokensBefore,
		"tokensAfter", result.EstimatedTokensAfter,
	)
	m.autosave(true)
	m.invalidateTranscript()
	m.invalidateChat()
	return nil
}
