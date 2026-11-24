package commands

func init() {
	Register(Command{
		Name:        "normal",
		Description: "Switch to NORMAL mode (asks before non-read-only tools)",
		Run:         func() Action { return ActionSetNormal },
	})
}
