package commands

func init() {
	Register(Command{
		Name:        "continue",
		Description: "Resume the current turn using the existing context",
		Run:         func() Action { return ActionContinue },
	})
}
