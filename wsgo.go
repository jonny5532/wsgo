package main

import (
	"net/http"

	"github.com/jonny5532/wsgo/internal/wsgo"
)

func main() {
	wsgo.StartupWsgo(func(mux *http.ServeMux) {
//		_, done := wsgo.PyEval(`__import__("wsgo").cron`)
//		done()
	})
}
