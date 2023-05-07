package wsgo

import (
	"errors"
	"time"
)

type SimpleScheduler struct {
	jobQueue		chan *RequestJob
}

func (sched *SimpleScheduler) HandleJob(job *RequestJob, timeout time.Duration) error {//, done chan bool) {
	sched.jobQueue <- job

	select {
	case <-job.done:
	case <-time.After(timeout):
		return errors.New("Job timed out without being handled!")
	}
	
	return nil
}

func (sched *SimpleScheduler) GrabJob() *RequestJob {
	return <- sched.jobQueue
}

func (sched *SimpleScheduler) JobFinished(job *RequestJob, elapsed int64, cpu_elapsed int64) {
	job.done <- true
}

func NewSimpleScheduler() *SimpleScheduler {
	return &SimpleScheduler{
		jobQueue: make(chan *RequestJob, 10),
	}
}
