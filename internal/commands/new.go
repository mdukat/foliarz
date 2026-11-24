package commands

func init() {
	Register(Command{
		Name:        "new",
		Description: "Start a new session",
		Run:         func() Action { return ActionNewSession },
	})
}
