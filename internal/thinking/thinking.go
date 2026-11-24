// Package thinking defines the thinking levels understood by Foliarz.
package thinking

import "slices"

// Thinking levels, from least to most reasoning.
const (
	Off     = "off"
	Minimal = "minimal"
	Low     = "low"
	Medium  = "medium"
	High    = "high"
	XHigh   = "xhigh"
	Max     = "max"
)

var levels = []string{Off, Minimal, Low, Medium, High, XHigh, Max}

// Levels returns all thinking levels from least to most reasoning.
func Levels() []string {
	return slices.Clone(levels)
}

// Valid reports whether level is a known thinking level.
func Valid(level string) bool {
	return slices.Contains(levels, level)
}

// Next returns the level following level among all thinking levels, wrapping
// from the highest level back to Off.
func Next(level string) string {
	return NextIn(level, levels)
}

// NextIn returns the level following level in levels, wrapping from the last
// level back to the first one. Unknown levels return the first level, and an
// empty levels list returns Off.
func NextIn(level string, levels []string) string {
	if len(levels) == 0 {
		return Off
	}
	i := slices.Index(levels, level)
	if i < 0 {
		return levels[0]
	}
	return levels[(i+1)%len(levels)]
}
