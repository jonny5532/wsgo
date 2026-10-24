package wsgo

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync/atomic"
	"syscall"
)

/*
#include <Python.h>
*/
import "C"

var requestCount atomic.Uint64
var timeoutCount atomic.Uint64
var droppedCount atomic.Uint64
var errorCount atomic.Uint64
var blockCount atomic.Uint64
var blockedCount atomic.Uint64

func PrintPythonTraceback() {
	if pythonFinalizing.Load() {
		return
	}
	runtime.LockOSThread()
	gilState := C.PyGILState_Ensure()
	printPythonTracebackLocked()
	C.PyGILState_Release(gilState)
	runtime.UnlockOSThread()
}

// printPythonTracebackLocked prints stack traces of all Python threads.
// It must be called with the GIL held.
func printPythonTracebackLocked() {
	runPythonCode(`
import faulthandler
faulthandler.dump_traceback(all_threads=True)
`)
}

func PrintRequestStats() {
	p := strconv.Itoa(process) + ":"
	
	fmt.Println(p, "Active requests:", scheduler.activeRequests.Load())
	fmt.Println(p, "Active requests by IP:")
	
	scheduler.activeRequestsBySourceMutex.Lock()
	for k, v := range scheduler.activeRequestsBySource {
		fmt.Println(p, " ", k + ":", v)
	}
	scheduler.activeRequestsBySourceMutex.Unlock()

	fmt.Println(p, "Request count:", requestCount.Load())
	fmt.Println(p, "Request errors:", errorCount.Load())
	fmt.Println(p, "Request timeouts:", timeoutCount.Load())
	fmt.Println(p, "Request drops:", droppedCount.Load())
	fmt.Println(p, "Blocks established:", blockCount.Load())
	fmt.Println(p, "Blocked requests:", blockedCount.Load())
}

func PrintMemoryStats() {
	var rtm runtime.MemStats

	// Read full mem stats
	runtime.ReadMemStats(&rtm)

	p := strconv.Itoa(process) + ":"
	fmt.Println(p, "Goroutine count:", runtime.NumGoroutine())
	fmt.Println(p, "Go allocated memory (in bytes):", rtm.Alloc)

	if pythonFinalizing.Load() {
		return
	}

	runtime.LockOSThread()
	gilState := C.PyGILState_Ensure()
	runPythonCode(`
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
