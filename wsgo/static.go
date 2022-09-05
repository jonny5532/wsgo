package wsgo

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func TryStatic(w http.ResponseWriter, req *http.Request) bool {
	if len(req.URL.Path) < 2 || req.URL.Path[0] != '/' || len(staticMap) == 0 {
		return false
	}

	for _, mapping := range staticMap {
		if strings.HasPrefix(req.URL.Path, mapping[0]) {
			local_prefix, err := filepath.Abs(mapping[1])
			if err != nil {
				continue
			}

			file_path := filepath.Join(local_prefix, req.URL.Path[len(mapping[0]):])

			file_path, err = filepath.Abs(file_path)
			if err != nil {
				continue
			}
			if !strings.HasPrefix(file_path, local_prefix) {
				continue
			}

			stat, err := os.Stat(file_path)
			if err != nil || stat.IsDir() {
				continue
			}

			// FIXME: very crude! need to check header format properly
			if strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") {
				// Is there a .gz suffixed file adjacent?
				gz_file_path := file_path + ".gz"
				gz_stat, err := os.Stat(gz_file_path)
				if err != nil || gz_stat.IsDir() {
					// not a file, pass
				} else {
					// use gzipped file instead
					// we have to figure out the Content-Type ourselves, else ServeContent will guess it wrong
					// only support extension-based guessing for now
					ctype := mime.TypeByExtension(filepath.Ext(file_path))
					if ctype != "" {
						file_path = gz_file_path
						w.Header().Set("Content-Type", ctype)
						w.Header().Set("Content-Encoding", "gzip")
						req.Header.Del("Range") // we don't support Range for static encoded responses
					}
				}
			}

			f, err := os.Open(file_path)
			if err != nil {
				continue
			}

			w.Header().Add("Expires", time.Now().Add(time.Duration(staticMaxAge)*time.Second).UTC().Format(time.RFC1123))
			w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(staticMaxAge))

			http.ServeContent(w, req, file_path, stat.ModTime(), f)

			return true
		}
	}
	return false
}
