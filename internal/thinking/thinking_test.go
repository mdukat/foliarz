package thinking

import (
	"reflect"
	"testing"
)

func TestLevels(t *testing.T) {
	want := []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}
	if got := Levels(); !reflect.DeepEqual(got, want) {
		t.Errorf("Levels() = %v, want %v", got, want)
	}
}

func TestValid(t *testing.T) {
	for _, level := range Levels() {
		if !Valid(level) {
			t.Errorf("Valid(%q) = false, want true", level)
		}
	}
	for _, level := range []string{"", "turbo", "OFF"} {
		if Valid(level) {
			t.Errorf("Valid(%q) = true, want false", level)
		}
	}
}

func TestNextCycles(t *testing.T) {
	want := map[string]string{
		Off:     Minimal,
		Minimal: Low,
		Low:     Medium,
		Medium:  High,
		High:    XHigh,
		XHigh:   Max,
		Max:     Off,
	}
	for level, next := range want {
		if got := Next(level); got != next {
			t.Errorf("Next(%q) = %q, want %q", level, got, next)
		}
	}
}

func TestNextUnknown(t *testing.T) {
	if got := Next("turbo"); got != Off {
		t.Errorf("Next(turbo) = %q, want off", got)
	}
}

func TestNextIn(t *testing.T) {
	levels := []string{Off, Low, High, Max}
	want := map[string]string{
		Off:  Low,
		Low:  High,
		High: Max,
		Max:  Off,
	}
	for level, next := range want {
		if got := NextIn(level, levels); got != next {
			t.Errorf("NextIn(%q) = %q, want %q", level, got, next)
		}
	}
}

func TestNextInUnknownLevel(t *testing.T) {
	levels := []string{Off, Low, High, Max}
	if got := NextIn(Medium, levels); got != Off {
		t.Errorf("NextIn(medium) = %q, want off", got)
	}
}

func TestNextInEmpty(t *testing.T) {
	if got := NextIn(Low, nil); got != Off {
		t.Errorf("NextIn(low, nil) = %q, want off", got)
	}
}
