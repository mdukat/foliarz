package commands

func init() {
	Register(Command{
		Name:        "quit",
		Description: "Close Foliarz",
		Run:         func() Action { return ActionQuit },
	})
}
