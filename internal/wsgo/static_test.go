package wsgo

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestPathHasPrefix(t *testing.T) {
	cases := []struct {
		path   string
		prefix string
		want   bool
	}{
		{"/media", "/media", true},
		{"/media/x", "/media", true},
		{"/media/", "/media", true},
		{"/mediaevil/x", "/media", false},
		{"/mediaevil", "/media", false},
		{"/x", "/", true},
		{"/", "/", true},
		{"/x", "", true},
		{"/mediax/media", "/media", false},
	}

	for _, c := range cases {
		if got := pathHasPrefix(c.path, c.prefix); got != c.want {
			t.Errorf("pathHasPrefix(%q, %q) = %v, want %v", c.path, c.prefix, got, c.want)
		}
	}
}

func TestIsPathWithin(t *testing.T) {
	cases := []struct {
		path string
		root string
		want bool
	}{
		{"/srv/web", "/srv/web", true},
		{"/srv/web/x", "/srv/web", true},
		{"/srv/web-secret/x", "/srv/web", false},
		{"/srv/websecret", "/srv/web", false},
		{"/etc/passwd", "/srv/web", false},
		{"/etc/passwd", "/", true},
	}

	for _, c := range cases {
		if got := isPathWithin(c.path, c.root); got != c.want {
			t.Errorf("isPathWithin(%q, %q) = %v, want %v", c.path, c.root, got, c.want)
		}
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"gzip", true},
		{"gzip, deflate", true},
		{"deflate, gzip;q=0.5", true},
		{"gzip;q=0", false},
		{"gzip; q=0", false},
		{"gzip;q=0.0", false},
		{"deflate", false},
		{"identity", false},
		{"*", true},
		{"*, gzip;q=0", false},
		{"br, *;q=0", false},
	}

	for _, c := range cases {
		if got := acceptsGzip(c.header); got != c.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}

func TestStaticContentType(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name    string
		content string
		want    string
	}{
		// Known to Go's built-in table, so not sniffed.
		{"style.css", "body {}", "text/css; charset=utf-8"},
		// An extension no MIME table knows, so the content is sniffed.
		{"notes.wsgotest", "just some text", "text/plain; charset=utf-8"},
		{"page.wsgotest", "<!DOCTYPE html><html></html>", "text/html; charset=utf-8"},
		{"empty.wsgotest", "", "text/plain; charset=utf-8"},
	}

	for _, c := range cases {
		path := filepath.Join(dir, c.name)
		if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}

		if got := staticContentType(path, f); got != c.want {
			t.Errorf("staticContentType(%q) = %q, want %q", c.name, got, c.want)
		}

		// The file must be left at the start, for serving.
		rest, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		if string(rest) != c.content {
			t.Errorf("staticContentType(%q) left the file at the wrong offset", c.name)
		}
		f.Close()
	}
}

func TestOpenRegular(t *testing.T) {
	dir := t.TempDir()

	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, _, err := openRegular(file)
	if err != nil {
		t.Fatalf("openRegular(regular file) failed: %v", err)
	}
	content, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(content) != "hello" {
		t.Fatalf("reading regular file got %q, %v", content, err)
	}

	if _, _, err := openRegular(dir); err == nil {
		t.Error("openRegular accepted a directory")
	}

	// Opening a FIFO with no writer would block forever without O_NONBLOCK.
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := openRegular(fifo)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("openRegular accepted a FIFO")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("openRegular blocked opening a FIFO")
	}
}
