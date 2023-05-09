package wsgo

import (
	"net/http"
)

func CanAccelResponse(job *RequestJob) bool {
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

	if v := job.w.Header().Get("X-WSGo-Retry"); v!="" {
		job.w.Header().Del("X-WSGo-Retry")
		// retrying only allowed if we're not already handling a retry
		if job.req.Header.Get("X-WSGo-Retry") != "" {
		 	job.retryId = v
			return true
		}
	}
	return false
}

func ResolveAccel(job *RequestJob) bool {
	if job.sendFile != "" {
		// this will always produce a 2xx response irrespective of the original statusCode
		http.ServeFile(job.w, job.req, job.sendFile)
		return true
	}
	if job.retryId != "" {
		DoRetry(job)
		return true
	}
	return false
}
