package main

import (
	"net/http"

	"github.com/jonny5532/wsgo/wsgo"
)

func main() {
	wsgo.StartupWsgo(func(mux *http.ServeMux) {})
}
