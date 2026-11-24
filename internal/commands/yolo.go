package commands

func init() {
	Register(Command{
		Name:        "yolo",
		Description: "Switch to YOLO mode (all tools without asking)",
		Run:         func() Action { return ActionSetYolo },
	})
}
