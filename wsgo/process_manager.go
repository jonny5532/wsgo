package wsgo

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

var EXITCODE_INVALID int = 27

func RunProcess(wg *sync.WaitGroup, process int) {
	for {
		args := append(os.Args[1:], "--process", strconv.Itoa(process))
		cmd := exec.Command(os.Args[0], args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(
			[]string{
				// Stop glibc's per-thread arenas eating all the RAM, and
				// encourage mmap use for allocations.
				"GLIBC_TUNABLES=glibc.malloc.arena_max=2 glibc.malloc.mmap_threshold=250000",
			},
			// Inherit parent process environment
			os.Environ()...,
		)
		cmd.Run()
		if cmd.ProcessState.Success() {
			log.Println("Process", process, "has finished.")
			break
		} else if cmd.ProcessState.ExitCode() == EXITCODE_INVALID {
			log.Println("Process", process, "could not start.")
			break
		}
		log.Println("Process", process, "exited uncleanly, restarting.")
		time.Sleep(100 * time.Millisecond)
	}
	wg.Done()
}

func RunProcessManager() {
	var wg sync.WaitGroup

	for i := 1; i <= processes; i++ {
		wg.Add(1)
		go RunProcess(&wg, i)
	}

	wg.Wait()
}

// Allow a process to exit without being restarted by the process manager.
func ExitProcessInvalid(msg string) {
	log.Println(msg)
	os.Exit(EXITCODE_INVALID)
}
