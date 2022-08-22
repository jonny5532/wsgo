package wsgo

import (
	"log"
	"time"
)

/*
#include <Python.h>
*/
import "C"

type Cron struct {
	name     string
	function *C.PyObject
	nextRun  time.Time

	// periodic task fields (overrides cron fields)
	period time.Duration

	// cron-style fields
	min  int
	hour int
	day  int
	mon  int
	wday int
}

var crons []Cron
var cronAdded chan bool

func init() {
	cronAdded = make(chan bool, 1)
}

func AddCron(function *C.PyObject, period int, min int, hour int, day int, mon int, wday int) {
	cron := Cron{
		period:   time.Duration(period) * time.Second,
		min:      min,
		hour:     hour,
		day:      day,
		mon:      mon,
		wday:     wday,
		function: function,
	}

	str := C.PyObject_Str(function)
	cron.name = C.GoString(C.PyUnicode_AsUTF8(str))
	C.Py_DecRef(str)

	cron.nextRun = cron.calculateNextRun()
	log.Println("Added cron job", cron.name, "with next run at", cron.nextRun)
	crons = append(crons, cron)

	// nonblocking send
	select {
	case cronAdded <- true:
	default:
	}
}

//export go_add_cron
func go_add_cron(function *C.PyObject, period C.long, min C.long, hour C.long, day C.long, mon C.long, wday C.long) {
	if process > 1 {
		// only add cron jobs on the first process
		return
	}

	C.Py_IncRef(function)
	AddCron(function, int(period), int(min), int(hour), int(day), int(mon), int(wday))
}

func CronRoutine() {
	for {
		d := DurationUntilNextCron()
		//log.Println("Cron: sleeping for", d)

		//wait for either the next deadline, or a new cron job to be added
		select {
		case <-cronAdded:
		case <-time.After(d + (100 * time.Millisecond)): //add 0.1s to avoid accidentally maxing the CPU
		}

		CronTick()
	}
}

func DurationUntilNextCron() time.Duration {
	if len(crons) == 0 {
		// wake up every 60s incase we missed a newly created job somehow
		return 60 * time.Second
	}

	now := time.Now()
	var lowest time.Duration
	for i, cron := range crons {
		if now.After(cron.nextRun) {
			return 0 * time.Second
		}
		d := cron.nextRun.Sub(now)
		if i == 0 || d < lowest {
			lowest = d
		}
	}
	return lowest
}

func CronTick() {
	now := time.Now()
	for i, cron := range crons {
		if now.After(cron.nextRun) {
			// time to run!
			//log.Println("Cron: running", cron.name)
			backgroundJobs <- &BackgroundJob{function: cron.function}
			crons[i].nextRun = crons[i].calculateNextRun()
		}
	}
}

func (cron *Cron) calculateNextRun() time.Time {
	nextRun := time.Now()

	if cron.period > 0 {
		// Periodic tasks will slip in time, since the gap between the previous
		// task ending and the next starting are the period, rather than the
		// time between starts
		return nextRun.Add(cron.period)
	}

	// FIXME - this is all pretty clumsy

	var cur int
	var next int

	if cron.mon > -1 {
		v := nextRun.Month()
		if time.Month(cron.mon) != v {
			nextRun = time.Date(nextRun.Year(), time.Month(cron.mon), 1, 0, 0, 0, 0, nextRun.Location())
			if time.Month(cron.mon) < v {
				nextRun = nextRun.AddDate(1, 0, 0)
			}
		}
	}
	if cron.wday > -1 {
		v := int(nextRun.Weekday())
		if cron.wday != v {
			nextRun = time.Date(nextRun.Year(), nextRun.Month(), nextRun.Day(), 0, 0, 0, 0, nextRun.Location())
			nextRun = nextRun.AddDate(0, 0, (7+(cron.wday-int(nextRun.Weekday())))%7)
		}
	}
	if cron.day > -1 {
		v := nextRun.Day()
		if cron.day != v {
			nextRun = time.Date(nextRun.Year(), nextRun.Month(), cron.day, 0, 0, 0, 0, nextRun.Location())
			if cron.day < v {
				nextRun = nextRun.AddDate(0, 1, 0)
			}
		}
	}
	if cron.hour > -1 {
		v := nextRun.Hour()
		if cron.hour != v {
			nextRun = time.Date(nextRun.Year(), nextRun.Month(), nextRun.Day(), cron.hour, 0, 0, 0, nextRun.Location())
			if cron.hour < v {
				nextRun = nextRun.AddDate(0, 0, 1)
			}
		}
	}
	if true {
		cur = nextRun.Minute()
		if cron.min < 0 {
			// -1 means every minute, -2 every second minute, etc
			next = (cur + (-cron.min) - (cur % -cron.min)) % 60
		} else {
			next = cron.min
		}
		nextRun = time.Date(nextRun.Year(), nextRun.Month(), nextRun.Day(), nextRun.Hour(), next, 0, 0, nextRun.Location())
		if cron.hour == -1 && next <= cur {
			nextRun = nextRun.Add(1 * time.Hour)
		}
	}

	return nextRun
}
