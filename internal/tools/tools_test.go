package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAll(t *testing.T) {
	all := All()
	want := map[string]bool{
		"bash":     false,
		"datetime": true,
		"edit":     false,
		"ls":       true,
		"read":     true,
		"write":    false,
	}
	if len(all) != len(want) {
		t.Fatalf("len(All()) = %d, want %d", len(all), len(want))
	}
	for i, tool := range all {
		readOnly, ok := want[tool.Name]
		if !ok {
			t.Errorf("unexpected tool %q", tool.Name)
			continue
		}
		if tool.ReadOnly != readOnly {
			t.Errorf("%s ReadOnly = %v, want %v", tool.Name, tool.ReadOnly, readOnly)
		}
		if !json.Valid(tool.Parameters) {
			t.Errorf("%s parameters are not valid JSON", tool.Name)
		}
		if i > 0 && all[i-1].Name > tool.Name {
			t.Errorf("tools are not sorted: %q before %q", all[i-1].Name, tool.Name)
		}
	}
}

func TestLookup(t *testing.T) {
	if _, ok := Lookup("datetime"); !ok {
		t.Error("Lookup(datetime) not found")
	}
	if _, ok := Lookup("does-not-exist"); ok {
		t.Error("Lookup(does-not-exist) found")
	}
}

func TestDatetimeRun(t *testing.T) {
	original := now
	defer func() { now = original }()

	fixed := time.Date(2026, time.November, 24, 14, 46, 12, 0, time.FixedZone("CET", 3600))
	now = func() time.Time { return fixed }

	tool, ok := Lookup("datetime")
	if !ok {
		t.Fatal("datetime tool not found")
	}
	got, err := tool.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantPrefix := "2026-11-24T14:46:12+01:00"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("Run() = %q, want prefix %q", got, wantPrefix)
	}
	if !strings.Contains(got, "Tuesday") {
		t.Errorf("Run() = %q, want weekday", got)
	}
}

func TestRegisterRejectsInvalidTools(t *testing.T) {
	valid := Tool{
		Name:        "valid",
		Description: "valid",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Run:         func(context.Context, json.RawMessage) (string, error) { return "", nil },
	}

	invalid := []Tool{
		{Name: "", Description: "x", Parameters: valid.Parameters, Run: valid.Run},
		{Name: "two words", Description: "x", Parameters: valid.Parameters, Run: valid.Run},
		{Name: "no-desc", Parameters: valid.Parameters, Run: valid.Run},
		{Name: "bad-schema", Description: "x", Parameters: json.RawMessage(`{`), Run: valid.Run},
		{Name: "no-run", Description: "x", Parameters: valid.Parameters},
	}
	for _, tool := range invalid {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%+v) did not panic", tool)
				}
			}()
			Register(tool)
		}()
	}

	defer func() {
		if recover() == nil {
			t.Error("Register() did not panic on duplicate name")
		}
	}()
	Register(Tool{
		Name:        "datetime",
		Description: "duplicate",
		Parameters:  valid.Parameters,
		Run:         valid.Run,
	})
}
