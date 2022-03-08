package main

import (
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
