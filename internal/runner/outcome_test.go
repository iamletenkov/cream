package runner

import "testing"

func TestClassify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code int
		want Outcome
	}{
		{0, OK},
		{10, Reboot},
		{20, NotApplicable},
		{1, Failed},
		{2, Failed},
		{137, Failed},
		{-1, Failed},
	}
	for _, tc := range tests {
		if got := Classify(tc.code); got != tc.want {
			t.Errorf("Classify(%d) = %v, ожидали %v", tc.code, got, tc.want)
		}
	}
}

func TestOutcomeString(t *testing.T) {
	t.Parallel()
	for o, want := range map[Outcome]string{OK: "ok", Reboot: "reboot", NotApplicable: "not-applicable", Failed: "failed"} {
		if got := o.String(); got != want {
			t.Errorf("%d.String() = %q, ожидали %q", int(o), got, want)
		}
	}
}
