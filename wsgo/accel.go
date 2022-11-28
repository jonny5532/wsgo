package wsgo

import (
	"net/http"
)

func CanAccelResponse(job *Job) bool {
	if v := job.w.Header().Get("X-Sendfile"); v!="" {
		job.sendFile = v
		job.w.Header().Del("X-Sendfile")
		return true
	}
	if v := job.w.Header().Get("X-Accel-Redirect"); v!="" {
		job.sendFile = v
		job.w.Header().Del("X-Accel-Redirect")
		return true
	}

	// if v := job.w.Header().Get("X-WSGo-Async"); v!="" {
	// 	job.asyncId = v
	// 	job.w.Header().Del("X-WSGo-Async")
	// 	return true
	// }
	return false
}

func ResolveAccel(job *Job) {
	if job.sendFile != "" {
		// this will always produce a 2xx response irrespective of the original statusCode
		http.ServeFile(job.w, job.req, job.sendFile)
	}
}
