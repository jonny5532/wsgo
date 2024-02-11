package wsgo

import (
	"fmt"
	"net"
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
	remoteAddr, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return "-"
	}

	remoteAddrIp := net.ParseIP(remoteAddr)	
	if remoteAddrIp != nil && (remoteAddrIp.IsLoopback() || remoteAddrIp.IsPrivate()) {
		// only use X-F-F header if remoteAddr is local/private
		f := strings.TrimSpace(strings.Split(req.Header.Get("X-Forwarded-For"), ",")[0])
		if f != "" {
			return f
		}
	}

	return remoteAddr
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

func LogRequestJob(job *RequestJob) {
	LogRequest(
		job.req,
		job.statusCode,
		job.finish,
		int(job.elapsed),
		int(job.cpuElapsed),
		job.worker,
		job.priority,
	)
}
