package wsgo

import (
	"strconv"
	"syscall"
	"unsafe"
)

// The manager progress will be called "wsgo" and the server processes "wsgo-N".
func processName(process int) string {
	if process == 0 {
		return "wsgo"
	}
	return "wsgo-" + strconv.Itoa(process)
}

// Max length is 15 bytes (the kernel will truncate longer names).
func setProcessName(name string) {
	buf := append([]byte(name), 0)
	syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_NAME,
		uintptr(unsafe.Pointer(&buf[0])), 0)
}
