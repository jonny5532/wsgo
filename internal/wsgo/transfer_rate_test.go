package wsgo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// setUpTransferRateTest shortens the transfer grace period so tests don't take
// long, restoring the limits when the test finishes.
func setUpTransferRateTest(t *testing.T) {
	oldRate, oldGrace, oldChunk := minTransferRate, transferGrace, transferChunkSize
	t.Cleanup(func() {
		minTransferRate, transferGrace, transferChunkSize = oldRate, oldGrace, oldChunk
	})
	minTransferRate = 4096
	transferGrace = 300 * time.Millisecond
	transferChunkSize = 32 * 1024
}

func dialTestServer(t *testing.T, srv *httptest.Server) net.Conn {
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func waitForError(t *testing.T, errc chan error) error {
	select {
	case err := <-errc:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the handler")
		return nil
	}
}

func TestStalledDownloadIsDropped(t *testing.T) {
	setUpTransferRateTest(t)

	errc := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := NewNonCachingCacheWriter(w)
		cw.WriteHeader(200)
		chunk := make([]byte, 1<<20)
		var err error
		for i := 0; i < 1024 && err == nil; i++ {
			_, err = cw.Write(chunk)
		}
		errc <- err
	}))
	defer srv.Close()

	// Request a large response and never read it.
	conn := dialTestServer(t, srv)
	fmt.Fprint(conn, "GET / HTTP/1.1\r\nHost: test\r\n\r\n")

	if err := waitForError(t, errc); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected the write to hit its deadline, got %v", err)
	}
}

func TestPausesBetweenWritesAreAllowed(t *testing.T) {
	setUpTransferRateTest(t)

	part := bytes.Repeat([]byte("a"), 100000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := NewNonCachingCacheWriter(w)
		cw.WriteHeader(200)
		cw.Write(part)
		time.Sleep(3 * transferGrace)
		cw.Write(part)
		time.Sleep(3 * transferGrace)
		cw.Finish()
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 2*len(part) {
		t.Fatalf("expected %d bytes, got %d", 2*len(part), len(body))
	}
}

// startUploadServer returns a server which reads the request body with the
// minimum transfer rate, sending the result of reading it to errc.
func startUploadServer(t *testing.T, errc chan error) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(newMinRateReader(r.Body, w))
		errc <- err
	}))
	t.Cleanup(srv.Close)
	return srv
}

// sendSlowly sends a request body in pieces, pausing between them.
func sendSlowly(conn net.Conn, pieces int, size int, pause time.Duration) {
	fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: %d\r\n\r\n", pieces*size)
	piece := bytes.Repeat([]byte("a"), size)
	for i := 0; i < pieces; i++ {
		time.Sleep(pause)
		if _, err := conn.Write(piece); err != nil {
			return
		}
	}
}

func TestStalledUploadIsDropped(t *testing.T) {
	setUpTransferRateTest(t)

	errc := make(chan error, 1)
	conn := dialTestServer(t, startUploadServer(t, errc))
	fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 1000000\r\n\r\nstart")

	if err := waitForError(t, errc); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected the read to hit its deadline, got %v", err)
	}
}

func TestUploadBelowMinimumRateIsDropped(t *testing.T) {
	setUpTransferRateTest(t)

	errc := make(chan error, 1)
	conn := dialTestServer(t, startUploadServer(t, errc))
	// About 1KB/s, a quarter of the minimum
	go sendSlowly(conn, 30, 100, 100*time.Millisecond)

	if err := waitForError(t, errc); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected the read to hit its deadline, got %v", err)
	}
}

func TestUploadAboveMinimumRateIsAllowed(t *testing.T) {
	setUpTransferRateTest(t)

	errc := make(chan error, 1)
	conn := dialTestServer(t, startUploadServer(t, errc))
	// About 10KB/s, for several times the grace period
	go sendSlowly(conn, 20, 1000, 100*time.Millisecond)

	if err := waitForError(t, errc); err != nil {
		t.Fatalf("expected the upload to succeed, got %v", err)
	}
}

func TestPausesBetweenReadsAreAllowed(t *testing.T) {
	setUpTransferRateTest(t)

	result := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := newMinRateReader(r.Body, w)
		first := make([]byte, 50000)
		if _, err := io.ReadFull(body, first); err != nil {
			result <- "first read: " + err.Error()
			return
		}
		time.Sleep(3 * transferGrace)
		rest, err := io.ReadAll(body)
		if err != nil {
			result <- "second read: " + err.Error()
			return
		}

		// The request should still be live after another pause.
		time.Sleep(3 * transferGrace)
		if err := r.Context().Err(); err != nil {
			result <- "context: " + err.Error()
			return
		}
		result <- fmt.Sprint(len(first) + len(rest))
	}))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/octet-stream", bytes.NewReader(make([]byte, 200000)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if got := <-result; got != "200000" {
		t.Fatalf("expected the whole body to be read, got %s", got)
	}
}

// Without a body, the server watches the connection for the client going
// away from the start, so reading the empty body mustn't leave a deadline set.
func TestReadingEmptyBodyLeavesNoDeadline(t *testing.T) {
	setUpTransferRateTest(t)

	result := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(newMinRateReader(r.Body, w))
		time.Sleep(3 * transferGrace)
		result <- r.Context().Err()
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if err := <-result; err != nil {
		t.Fatalf("expected the request context to still be live, got %v", err)
	}
}
