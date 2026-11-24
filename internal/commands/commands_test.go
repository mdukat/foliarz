package commands

import (
	"reflect"
	"testing"
)

func TestAll(t *testing.T) {
	var names []string
	for _, cmd := range All() {
		names = append(names, cmd.Name)
		if cmd.Description == "" {
			t.Errorf("command %q has no description", cmd.Name)
		}
	}
	want := []string{"compact", "continue", "exit", "folia", "model", "new", "normal", "quit", "session", "yolo"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("All() names = %v, want %v", names, want)
	}
}

func TestCommandActions(t *testing.T) {
	want := map[string]Action{
		"quit":     ActionQuit,
		"exit":     ActionQuit,
		"new":      ActionNewSession,
		"folia":    ActionSetFolia,
		"normal":   ActionSetNormal,
		"continue": ActionContinue,
		"yolo":     ActionSetYolo,
		"session":  ActionListSessions,
		"compact":  ActionCompact,
		"model":    ActionSelectModel,
	}
	for name, action := range want {
		cmd, ok := Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) not found", name)
		}
		if got := cmd.Run(); got != action {
			t.Errorf("%q action = %v, want %v", name, got, action)
		}
	}
}

func TestLookup(t *testing.T) {
	if _, ok := Lookup("quit"); !ok {
		t.Error("Lookup(quit) not found")
	}
	if _, ok := Lookup("does-not-exist"); ok {
		t.Error("Lookup(does-not-exist) found")
	}
}

func TestMatch(t *testing.T) {
	tests := map[string][]string{
		"":     {"compact", "continue", "exit", "folia", "model", "new", "normal", "quit", "session", "yolo"},
		"m":    {"model"},
		"c":    {"compact", "continue"},
		"q":    {"quit"},
		"e":    {"exit"},
		"n":    {"new", "normal"},
		"f":    {"folia"},
		"s":    {"session"},
		"y":    {"yolo"},
		"quit": {"quit"},
		"QU":   {"quit"},
		"z":    nil,
	}
	for prefix, want := range tests {
		var names []string
		for _, cmd := range Match(prefix) {
			names = append(names, cmd.Name)
		}
		if !reflect.DeepEqual(names, want) {
			t.Errorf("Match(%q) = %v, want %v", prefix, names, want)
		}
	}
}

func TestRegisterRejectsDuplicates(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Register() did not panic on duplicate name")
		}
	}()
	Register(Command{
		Name: "quit",
		Run:  func() Action { return ActionQuit },
	})
}

func TestRegisterRejectsInvalidCommands(t *testing.T) {
	invalid := []Command{
		{Name: "", Run: func() Action { return ActionNone }},
		{Name: "/quit", Run: func() Action { return ActionNone }},
		{Name: "two words", Run: func() Action { return ActionNone }},
		{Name: "valid-name", Run: nil},
	}
	for _, cmd := range invalid {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%+v) did not panic", cmd)
				}
			}()
			Register(cmd)
		}()
	}
}
