package runner

// Outcome is what a hook's exit code means to cream (spec §6.2).
type Outcome int

const (
	// OK — exit code 0.
	OK Outcome = iota
	// Reboot — exit code 10: success, a reboot is required.
	Reboot
	// NotApplicable — exit code 20: the hook does not apply in this environment.
	NotApplicable
	// Failed — any other code, including death by a signal (128+N) and timeout.
	Failed
)

// Hook exit codes with a meaning of their own (spec §6.2).
const (
	CodeReboot        = 10
	CodeNotApplicable = 20
)

// Classify maps a hook exit code to its Outcome.
func Classify(code int) Outcome {
	switch code {
	case 0:
		return OK
	case CodeReboot:
		return Reboot
	case CodeNotApplicable:
		return NotApplicable
	default:
		return Failed
	}
}

// String returns "ok", "reboot", "not-applicable" or "failed".
func (o Outcome) String() string {
	switch o {
	case OK:
		return "ok"
	case Reboot:
		return "reboot"
	case NotApplicable:
		return "not-applicable"
	default:
		return "failed"
	}
}
