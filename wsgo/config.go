package wsgo

import (
	"errors"
	"flag"
	"strings"
)

type staticMapping [][2]string

func (i *staticMapping) String() string {
	return "?"
}

func (i *staticMapping) Set(value string) error {
	bits := strings.SplitN(value, "=", 2)
	if len(bits) != 2 {
		return errors.New("Usage: --static-map /path=./localpath")
	}
	*i = append(*i, [2]string{bits[0], bits[1]})
	return nil
}

type heavyPrefix []string

func (i *heavyPrefix) String() string {
	return "?"
}

func (i *heavyPrefix) Set(value string) error {
	*i = append(*i, value)
	return nil
}

var totalWorkers int = 16     //total number of worker threads
var processes int = 1
var process int = 0
var bindAddress string = ":8000"
var wsgiModule string = "wsgi_app"
var requestTimeout int = 60
var backgroundTimeout int = 1800
var maxQueueLength int = 128
var requestBufferLength int = 1048576
// response buffering involves an extra copy so often isn't a performance gain
var responseBufferLength int = 0 //1048576
var maxAge int = 0
var maxAgeBeforeRefetch int = 0
var pageCacheLimit uint64 = 67108864
var staticMap staticMapping
var staticMaxAge int = 86400

func ParseFlags() {
	flag.IntVar(&totalWorkers, "workers", totalWorkers, "total number of worker threads")
	flag.IntVar(&processes, "processes", processes, "number of processes")
	flag.IntVar(&process, "process", process, "process number (internal)")
	flag.StringVar(&wsgiModule, "module", wsgiModule, "WSGI module to serve")
	flag.StringVar(&bindAddress, "http-socket", bindAddress, "server bind address")
	flag.IntVar(&requestTimeout, "request-timeout", requestTimeout, "request timeout in seconds")
	flag.IntVar(&maxAge, "max-age", maxAge, "maximum number of seconds to cache responses (0 to disable)")
	maxAgeBeforeRefetch = maxAge / 2
	flag.Uint64Var(&pageCacheLimit, "cache-size", pageCacheLimit, "maximum size of page cache in bytes")
	flag.Var(&staticMap, "static-map", "static file folder mapping")
	flag.IntVar(&staticMaxAge, "static-max-age", staticMaxAge, "encourage clients to cache static files for this many seconds (0 to disable)")
	flag.Parse()
}
