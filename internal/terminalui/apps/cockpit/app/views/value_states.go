package views

const (
	ValueRequired = "required"
	ValueAuto     = "auto"
	ValueBlocked  = "blocked"
)

func ValueWithDefault(value string) string {
	if value == "" {
		return "default"
	}
	return value + " (default)"
}
