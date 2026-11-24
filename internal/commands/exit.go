package commands

func init() {
	Register(Command{
		Name:        "exit",
		Description: "Close Foliarz",
		Run:         func() Action { return ActionQuit },
	})
}
