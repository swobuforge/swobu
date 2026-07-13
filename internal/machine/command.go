package machine

// Command is a marker for all commands in the machine.
// Commands are external effects.
// If it does not touch the outside world, it is not a command.
type Command interface{}
