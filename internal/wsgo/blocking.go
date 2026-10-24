package wsgo

import (
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

var blocked map[string]time.Time
var blockedCountByIp map[string]int
var blockedMutex sync.Mutex
var blockedCountByIpMutex sync.Mutex
var waitSlots chan bool

// The maximum number of requests that can be delayed simultaneously. If set too
// high we might exhaust ports or open file handles.
var WAIT_SLOT_COUNT int = 100

func init() {
	blocked = make(map[string]time.Time)
	blockedCountByIp = make(map[string]int)
	waitSlots = make(chan bool, WAIT_SLOT_COUNT)

	// Periodically clean stale blocks
	go func() {
		for {
			time.Sleep(60 * time.Second)
			ExpireStaleBlocks()
		}
	}()
}

func ExpireBlock(ip string) {
	// assumes that blockedMutex is already held
	blockedCountByIpMutex.Lock()
	log.Println("- unblocked", ip, "after", blockedCountByIp[ip], "blocked requests")
	delete(blocked, ip)
	delete(blockedCountByIp, ip)
	blockedCountByIpMutex.Unlock()
}

func ExpireStaleBlocks() {
	// Remove any blocks that have expired.
	
	now := time.Now()

	blockedMutex.Lock()
	for ip, t := range blocked {
		if now.After(t) {
			ExpireBlock(ip)
		}
	}
	blockedMutex.Unlock()
}

func TryBlocking(w http.ResponseWriter, req *http.Request) bool {
	// Check whether a request is from a blocked address.
	// If it is, respond and return true.
	// Otherwise return false.
	
	ip := GetRemoteAddr(req)

	blockedMutex.Lock()
	if blocked[ip] != (time.Time{}) {
		if time.Now().Before(blocked[ip]) {
			delay := int(math.Floor(time.Until(blocked[ip]).Seconds()))
			blockedMutex.Unlock()

			BlockDelay(delay)

			http.Error(w, "", 429)

			blockedCountByIpMutex.Lock()
			blockedCountByIp[ip]++
			blockedCountByIpMutex.Unlock()

			blockedCount.Add(1)
			return true
		}
		// Block has expired, remove the entry
		ExpireBlock(ip)
	}
	blockedMutex.Unlock()

	return false
}

func BlockDelay(maxDelay int) {
	// Wait before responding to a blocked request, to attempt to slow down
	// requests from misbehaving bots.

	select {
	case waitSlots <- true:
		delay := 25
		if maxDelay < 25 {
			delay = maxDelay
		}
		time.Sleep(time.Duration(delay) * time.Second)
		<-waitSlots
	default:
		// No wait slots available, return immediately
		return
	}
}

func UpdateBlocking(job *RequestJob) {
	// After a job has completed, check whether it set a blocking header.
	// If it did, block the IP address for the specified number of seconds.
	
	if v := job.w.Header().Get("X-WSGo-Block"); v!="" {
		seconds, err := strconv.Atoi(v)
		if err == nil {
			Block(job.req, seconds)
		}
		job.w.Header().Del("X-WSGo-Block")
	}
}

func Block(req *http.Request, seconds int) {
	// Block a request's source IP address for a specified number of seconds.

	ip := GetRemoteAddr(req)
	if ip == "-" {
		return
	}

	parsedIp := net.ParseIP(ip)
	if parsedIp == nil || parsedIp.IsLoopback() || parsedIp.IsPrivate() {
		// Don't block internal addresses
		return
	}
	
	log.Println("- blocking", ip, "for", seconds, "seconds")

	blockedMutex.Lock()
	blocked[ip] = time.Now().Add(time.Duration(seconds) * time.Second)
	blockedMutex.Unlock()
	blockCount.Add(1)
}
