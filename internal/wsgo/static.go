package wsgo

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// pathHasPrefix reports whether path is within the URL prefix, on segment
// boundaries. So "/media" matches "/media" and "/media/x" but not "/mediaevil".
func pathHasPrefix(path string, prefix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	if prefix == "" || prefix == "/" {
		return true
	}
	rest := path[len(prefix):]
	return rest == "" || rest[0] == '/'
}

// isPathWithin reports whether path is root itself or a path below it. Both
// paths must be absolute and cleaned (eg, after filepath.Abs/EvalSymlinks).
func isPathWithin(path string, root string) bool {
	if path == root {
		return true
	}
	if root == string(os.PathSeparator) {
		return true
	}
	return strings.HasPrefix(path, root+string(os.PathSeparator))
}

// acceptsGzip does a small RFC 9110 Accept-Encoding check, honouring
// quality values, so "gzip;q=0" means gzip is not acceptable.
func acceptsGzip(header string) bool {
	if header == "" {
		return false
	}

	gzipQ := -1.0
	starQ := -1.0

	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		name := part
		q := 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			name = strings.TrimSpace(part[:i])
			for _, param := range strings.Split(part[i+1:], ";") {
				param = strings.TrimSpace(param)
				if strings.HasPrefix(param, "q=") {
					if v, err := strconv.ParseFloat(strings.TrimSpace(param[2:]), 64); err == nil {
						q = v
					}
				}
			}
		}

		switch strings.ToLower(name) {
		case "gzip":
			gzipQ = q
		case "*":
			starQ = q
		}
	}

	if gzipQ >= 0 {
		return gzipQ > 0
	}
	if starQ >= 0 {
		return starQ > 0
	}
	return false
}

// openRegular opens path (which has already been resolved) and returns the
// open file and its FileInfo, rejecting directories. Opening before stating
// avoids a TOCTOU on the file type.
// staticContentType returns the Content-Type for the file at path, open as f,
// from its extension, or else by sniffing its contents (as ServeContent does).
// The system MIME tables may be missing (eg. slim container images), and Go's
// built-in table is small. f is left at the start, and "" is returned if it
// can't be read.
func staticContentType(path string, f *os.File) string {
	if ctype := mime.TypeByExtension(filepath.Ext(path)); ctype != "" {
		return ctype
	}

	var buf [512]byte
	n, err := io.ReadFull(f, buf[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return ""
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	return http.DetectContentType(buf[:n])
}

func openRegular(path string) (*os.File, os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		f.Close()
		if err == nil {
			err = os.ErrNotExist
		}
		return nil, nil, err
	}
	return f, stat, nil
}

func TryStatic(w http.ResponseWriter, req *http.Request) bool {
	if len(req.URL.Path) < 2 || req.URL.Path[0] != '/' || len(staticMap) == 0 {
		return false
	}

	// Only serve static files for safe methods; anything else (POST, PUT,
	// DELETE, ...) must reach the WSGI application.
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}

	for _, mapping := range staticMap {
		if !pathHasPrefix(req.URL.Path, mapping[0]) {
			continue
		}

		local_prefix, err := filepath.Abs(mapping[1])
		if err != nil {
			continue
		}
		// Resolve symlinks in the mapping root, so the containment check
		// below compares real paths.
		local_prefix, err = filepath.EvalSymlinks(local_prefix)
		if err != nil {
			continue
		}

		file_path := filepath.Join(local_prefix, req.URL.Path[len(mapping[0]):])
		file_path, err = filepath.Abs(file_path)
		if err != nil {
			continue
		}
		// Follow symlinks before checking containment: static files must
		// reside within the configured root even after resolution.
		file_path, err = filepath.EvalSymlinks(file_path)
		if err != nil {
			continue
		}
		if !isPathWithin(file_path, local_prefix) {
			continue
		}

		f, stat, err := openRegular(file_path)
		if err != nil {
			continue
		}

		// Is there a .gz suffixed file adjacent, and does the client
		// explicitly accept gzip?
		if acceptsGzip(req.Header.Get("Accept-Encoding")) {
			gz_path, gz_err := filepath.EvalSymlinks(file_path + ".gz")
			if gz_err == nil && isPathWithin(gz_path, local_prefix) {
				gz_file, gz_stat, gz_err := openRegular(gz_path)
				if gz_err == nil {
					// We have to figure out the Content-Type ourselves, else
					// ServeContent will guess it from the compressed bytes.
					ctype := staticContentType(file_path, f)
					if ctype != "" {
						// use gzipped file instead
						f.Close()
						f = gz_file
						stat = gz_stat
						w.Header().Set("Content-Type", ctype)
						w.Header().Set("Content-Encoding", "gzip")
						req.Header.Del("Range") // we don't support Range for static encoded responses
					} else {
						gz_file.Close()
					}
				}
			}
		}
		defer f.Close()

		// The response depends on Accept-Encoding, so caches must key on it.
		w.Header().Add("Vary", "Accept-Encoding")

		w.Header().Add("Expires", time.Now().Add(time.Duration(staticMaxAge)*time.Second).UTC().Format(time.RFC1123))
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(staticMaxAge))

		http.ServeContent(w, req, file_path, stat.ModTime(), f)

		return true
	}
	return false
}
