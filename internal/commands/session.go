package commands

func init() {
	Register(Command{
		Name:        "session",
		Description: "Open a previously saved session",
		Run:         func() Action { return ActionListSessions },
	})
}
