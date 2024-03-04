package wsgo

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync/atomic"
	"syscall"
	"unsafe"
)

/*
#include <Python.h>
*/
import "C"

var requestCount atomic.Uint64
var timeoutCount atomic.Uint64
var droppedCount atomic.Uint64
var errorCount atomic.Uint64

func PrintPythonTraceback() {
	runtime.LockOSThread()
	gilState := C.PyGILState_Ensure()
	cmd := C.CString(`
import faulthandler
faulthandler.dump_traceback(all_threads=True)
`)
	defer C.free(unsafe.Pointer(cmd))
	C.PyRun_SimpleStringFlags(cmd, nil)
	C.PyGILState_Release(gilState)
	runtime.UnlockOSThread()
}

func PrintRequestStats() {
	p := strconv.Itoa(process) + ":"
	fmt.Println(p, "Request count:", requestCount.Load())
	fmt.Println(p, "Request errors:", errorCount.Load())
	fmt.Println(p, "Request timeouts:", timeoutCount.Load())
	fmt.Println(p, "Request drops:", droppedCount.Load())
}

func PrintMemoryStats() {
	var rtm runtime.MemStats

	// Read full mem stats
	runtime.ReadMemStats(&rtm)

	p := strconv.Itoa(process) + ":"
	fmt.Println(p, "Goroutine count:", runtime.NumGoroutine())
	fmt.Println(p, "Go allocated memory (in bytes):", rtm.Alloc)

	runtime.LockOSThread()
	gilState := C.PyGILState_Ensure()
	cmd := C.CString(`
import threading
print("`+p+` Python live Thread count:", threading.active_count())

import gc
gc.collect()

try:
	from pympler import muppy
	print("`+p+` Python allocated memory (in bytes):", muppy.get_size(muppy.get_objects(include_frames=True)))
except ImportError:
	pass
`)
	defer C.free(unsafe.Pointer(cmd))
	C.PyRun_SimpleStringFlags(cmd, nil)
	C.PyGILState_Release(gilState)
	runtime.UnlockOSThread()
}

func NewMonitor() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR1, syscall.SIGUSR2)

	for {
		s := <-sigs
		if s==syscall.SIGUSR1 {
			PrintPythonTraceback()
		} else {
			PrintRequestStats()
			PrintMemoryStats()
		}
	}
}
