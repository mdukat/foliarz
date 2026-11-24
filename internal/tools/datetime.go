package tools

import (
	"context"
	"encoding/json"
	"time"
)

// now is the clock used by the datetime tool; tests replace it.
var now = time.Now

func init() {
	Register(Tool{
		Name:        "datetime",
		Description: "Get the current date and time from the operating system",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		ReadOnly:    true,
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			current := now()
			return current.Format(time.RFC3339) + " (" + current.Weekday().String() + ")", nil
		},
	})
}
