package wsgo

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func firstNonEmpty(a string, b string, c string) string {
	if a != "" {
		return a
	} else if b != "" {
		return b
	}
	return c
}

func GetRemoteAddr(req *http.Request) string {
	remoteAddr := req.RemoteAddr
	remoteAddrColonIndex := strings.LastIndex(remoteAddr, ":")
	if remoteAddrColonIndex > -1 {
		remoteAddr = remoteAddr[:remoteAddrColonIndex]
	}
	return firstNonEmpty(
		strings.Split(req.Header.Get("X-Forwarded-For"), ", ")[0],
		remoteAddr,
		"-",
	)
}

func LogRequest(req *http.Request, statusCode int, finishTime time.Time, elapsed int, cpuTime int, workerNumber int, priority int) {
	fmt.Println(
		GetRemoteAddr(req),
		strconv.Itoa(process)+":"+strconv.Itoa(workerNumber), "-",
		"["+finishTime.Format("02/Jan/2006:15:04:05 -0700")+"]",
		"\""+req.Method+" "+req.RequestURI+" "+req.Proto+"\"",
		strconv.Itoa(statusCode),
		"-",
		"\""+firstNonEmpty(req.Header.Get("Referer"), "-", "-")+"\"",
		"\""+firstNonEmpty(req.Header.Get("User-Agent"), "-", "-")+"\"",
		strconv.Itoa(elapsed)+"ms",
		strconv.Itoa(cpuTime)+"ms",
		strconv.Itoa(priority),
	)
}
