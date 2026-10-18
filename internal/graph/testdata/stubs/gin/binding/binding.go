package binding

type Binding interface{ Name() string }

type jsonBinding struct{}

func (jsonBinding) Name() string { return "json" }

var JSON Binding = jsonBinding{}
