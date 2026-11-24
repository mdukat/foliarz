package commands

func init() {
	Register(Command{
		Name:        "folia",
		Description: "Switch to FOLIA mode (read-only tools)",
		Run:         func() Action { return ActionSetFolia },
	})
}
