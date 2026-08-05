package service

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/Ken-Chy129/agent-master/internal/config"
)

// ManagedPID is the discriminator the doctor's version-mismatch diagnosis rests
// on: comparing it against the pid that holds the port separates "the service is
// merely an older build, restart it" from "an orphan is squatting the port, kill
// it". Those need opposite fixes, so a regression here silently restores wrong
// advice.
//
// The check is environment-dependent by nature, so it skips where no service is
// installed (CI). Where one is, it asserts the invariant that must hold — a
// reported pid names a process that exists — and only logs the comparison,
// because a genuinely broken machine should not turn the suite red.
func TestManagedPIDNamesALiveProcess(t *testing.T) {
	if !Installed() {
		t.Skip("no agent-master service installed on this machine")
	}

	managed, known := ManagedPID()
	t.Logf("ManagedPID = %d (known=%v)", managed, known)

	if !known {
		t.Skipf("could not query the service manager on %s", os.Getenv("GOOS"))
	}
	if managed < 0 {
		t.Fatalf("ManagedPID = %d, want a non-negative pid", managed)
	}
	if managed > 0 {
		// Signal 0 probes existence without touching the process.
		if err := syscall.Kill(managed, 0); err != nil && err != syscall.EPERM {
			t.Errorf("ManagedPID reported pid %d, which does not exist: %v", managed, err)
		}
	}

	serving, ok := servingPIDForTest(t)
	if !ok {
		t.Log("no pidfile — nothing is currently serving")
		return
	}
	t.Logf("serving pid = %d; same process = %v", serving, serving == managed)
}

func servingPIDForTest(t *testing.T) (int, bool) {
	t.Helper()
	path, err := config.PIDPath()
	if err != nil {
		return 0, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return pid, true
}
