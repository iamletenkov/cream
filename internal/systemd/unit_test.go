package systemd

import (
	"strings"
	"testing"
)

func TestUnitTemplate(t *testing.T) {
	t.Parallel()
	got := string(UnitTemplate())
	for _, want := range []string{
		"[Unit]\n",
		"ConditionPathExists=/var/lib/cream/installed/%i.json\n",
		"Type=oneshot\n",
		"RemainAfterExit=yes\n",
		"ExecStart=/usr/bin/cream configure --from-unit %i\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("template lacks %q", want)
		}
	}
	// The caller gets a copy: changing it does not change the embedded unit.
	b := UnitTemplate()
	b[0] = 'X'
	if UnitTemplate()[0] != '[' {
		t.Error("UnitTemplate returns shared slice")
	}
}
