package startup

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// processStart asks the kernel for this process's start time, which macOS
// keeps as wall-clock time to the microsecond.
func processStart(time.Time) (time.Time, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", os.Getpid())
	if err != nil {
		return time.Time{}, fmt.Errorf("reading the process start time: %w", err)
	}
	return time.Unix(info.Proc.P_starttime.Unix()), nil
}
