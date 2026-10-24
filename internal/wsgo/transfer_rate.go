package wsgo

import (
	"io"
	"net/http"
	"time"
)

// Clients must send request bodies and receive responses at a minimum average
// rate, so that a slow (or deliberately stalling) client can't hold on to a
// worker. Only time spent waiting on the client counts, so applications can
// take as long as they like between reads and writes.
//
// Each byte transferred earns 1/minTransferRate seconds of time to wait for
// the client, up to transferGrace, which also covers the initial wait and any
// brief stalls.
var minTransferRate = 4096 // bytes per second
var transferGrace = 30 * time.Second

// Large writes are split into pieces of this size, so that a slow client has
// to keep up throughout rather than only by the end of each write.
var transferChunkSize = 32 * 1024

// A transferRateLimit enforces the minimum transfer rate in one direction of a
// connection, using deadlines on the connection.
type transferRateLimit struct {
	setDeadline func(time.Time) error
	allowance   time.Duration
}

func newTransferRateLimit(setDeadline func(time.Time) error) *transferRateLimit {
	return &transferRateLimit{
		setDeadline: setDeadline,
		allowance:   transferGrace,
	}
}

// do runs an operation that transfers data with the client, with a deadline of
// the remaining allowance, and then updates the allowance by the time it took
// and the number of bytes it transferred.
func (l *transferRateLimit) do(op func() (int, error)) (int, error) {
	start := time.Now()
	if l.setDeadline(start.Add(l.allowance)) != nil {
		// Deadlines aren't supported by this connection
		return op()
	}

	n, err := op()

	earned := time.Duration(n) * time.Second / time.Duration(minTransferRate)
	l.allowance += earned - time.Since(start)
	if l.allowance > transferGrace {
		l.allowance = transferGrace
	}
	return n, err
}

// A minRateReader reads a request body, enforcing the minimum transfer rate.
type minRateReader struct {
	r     io.Reader
	rc    *http.ResponseController
	limit *transferRateLimit
}

func newMinRateReader(body io.Reader, w http.ResponseWriter) *minRateReader {
	rc := http.NewResponseController(w)
	return &minRateReader{
		r:     body,
		rc:    rc,
		limit: newTransferRateLimit(rc.SetReadDeadline),
	}
}

func (r *minRateReader) Read(p []byte) (int, error) {
	n, err := r.limit.do(func() (int, error) {
		return r.r.Read(p)
	})
	// Don't leave the deadline set whilst the body isn't being read. Once it
	// has been read, the server reads from the connection in the background
	// to notice if the client goes away, and would take the deadline passing
	// as a disconnection.
	r.rc.SetReadDeadline(time.Time{})
	return n, err
}
