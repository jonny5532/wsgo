package wsgo

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"unsafe"
)

/*
#include <Python.h>
*/
import "C"

func PrintMemoryStats() {
	var rtm runtime.MemStats

	// Read full mem stats
	runtime.ReadMemStats(&rtm)

	fmt.Println("Goroutine count:", runtime.NumGoroutine())
	fmt.Println("Go allocated memory (in bytes):", rtm.Alloc)

	runtime.LockOSThread()
	gilState := C.PyGILState_Ensure()
	cmd := C.CString(`
import threading
print("Python live Thread count:", threading.active_count())

import gc
gc.collect()

from pympler import muppy
print("Python allocated memory (in bytes):", muppy.get_size(muppy.get_objects(include_frames=True)))
`)
	defer C.free(unsafe.Pointer(cmd))
	C.PyRun_SimpleStringFlags(cmd, nil)
	C.PyGILState_Release(gilState)
	runtime.UnlockOSThread()
}

func NewMonitor() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR2)

	for {
		<-sigs

		PrintMemoryStats()
	}
}
