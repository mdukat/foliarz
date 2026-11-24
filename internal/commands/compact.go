package commands

func init() {
	Register(Command{
		Name:        "compact",
		Description: "Summarize older context to free up the context window",
		Run:         func() Action { return ActionCompact },
	})
}
