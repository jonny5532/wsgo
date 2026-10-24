package wsgo

import (
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

var EXITCODE_INVALID int = 27

var processManagerStopping bool = false
var processCommands []*exec.Cmd
var processCommandsMutex sync.Mutex

// The Python interpreter and the command-line arguments (excluding the
// program name) that started us, used to spawn the server processes.
var pythonExecutable string
var serverArgs []string

func removeProcessCommand(cmd *exec.Cmd) {
	processCommandsMutex.Lock()
	defer processCommandsMutex.Unlock()
	for i, c := range processCommands {
		if c == cmd {
			processCommands = append(processCommands[:i], processCommands[i+1:]...)
			break
		}
	}
}

func RunProcess(wg *sync.WaitGroup, process int) {
	defer wg.Done()

	for {
		args := append([]string{"-m", "wsgo"}, serverArgs...)
		args = append(args, "--process", strconv.Itoa(process))
		cmd := exec.Command(pythonExecutable, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(
			[]string{
				// Stop glibc's per-thread arenas eating all the RAM, and
				// encourage mmap use for allocations.
				"GLIBC_TUNABLES=glibc.malloc.arena_max=2:glibc.malloc.mmap_threshold=250000",
				// Don't buffer stdout/stderr, so logs appear promptly.
				"PYTHONUNBUFFERED=1",
			},
			// Inherit parent process environment
			os.Environ()...,
		)

		processCommandsMutex.Lock()
		processCommands = append(processCommands, cmd)
		shouldExit := processManagerStopping
		processCommandsMutex.Unlock()

		if shouldExit {
			log.Println("Process", process, "not started.")
			break
		}

		err := cmd.Run()

		if cmd.ProcessState == nil {
			// The process never started, so there is no state to inspect
			// and no Process to signal. Retrying can't help.
			log.Println("Process", process, "could not be executed:", err)
			removeProcessCommand(cmd)
			break
		}

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
}

func RunProcessManager() {
	var wg sync.WaitGroup

	for i := 1; i <= processes; i++ {
		wg.Add(1)
		go RunProcess(&wg, i)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		processCommandsMutex.Lock()
		processManagerStopping = true
		defer processCommandsMutex.Unlock()

		for _, cmd := range processCommands {
			if cmd.Process != nil {
				cmd.Process.Signal(syscall.SIGTERM)
			}
		}
	}()

	wg.Wait()
}

// Allow a process to exit without being restarted by the process manager.
func ExitProcessInvalid(msg string) {
	log.Println(msg)
	os.Exit(EXITCODE_INVALID)
}
