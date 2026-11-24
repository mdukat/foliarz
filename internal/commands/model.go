package commands

func init() {
	Register(Command{
		Name:        "model",
		Description: "Switch the model for the current session",
		Run:         func() Action { return ActionSelectModel },
	})
}
