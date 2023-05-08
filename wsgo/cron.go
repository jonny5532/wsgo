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
		wday:     wday % 7,
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
			// Unbuffered channel, so cron will block
			backgroundJobs <- &BackgroundJob{function: cron.function}
			crons[i].nextRun = crons[i].calculateNextRun()
		}
	}
}

func Iif(condition bool, ifTrue int, ifFalse int) int {
	if condition {
		return ifTrue
	} else {
		return ifFalse
	}
}

func (cron *Cron) calculateNextRun() time.Time {
	now := time.Now()

	if cron.period > 0 {
		// Periodic tasks will slip in time, since the gap between the previous
		// task ending and the next starting are the period, rather than the
		// time between starts
		return now.Add(cron.period)
	}

	if false {
		// for testing
		now = time.Date(
			now.Year(), 
			now.Month(),
			now.Day(),
			7, 
			21, 
			0, 0, now.Location(),
		)
	}
	
	// Calculate the 'nextRun' based on the current time, replacing any of
	// the time segments with those specified in the cron rule. The resulting
	// time may be in the future (fine), but it may also be in the past, in
	// which case it will need advancing.

	nextRun := time.Date(
		now.Year(), 
		time.Month(Iif(cron.mon > -1, cron.mon, int(now.Month()))),
		Iif(cron.day > -1, cron.day, now.Day()),
		Iif(cron.hour > -1, cron.hour, now.Hour()), 
		Iif(cron.min > -1, cron.min, now.Minute()), 
		0, 0, now.Location(),
	)

	// nextRun is currently in the past, so try advancing the minute (if it isn't specified)

	if !nextRun.After(now) && cron.min == -1 {
		// try the next minute
		v := nextRun.Minute() + 1
		nextRun = time.Date(nextRun.Year(), nextRun.Month(), nextRun.Day(), nextRun.Hour(), v%60, 0, 0, nextRun.Location())
		if v >= 60 && cron.hour == -1 && (nextRun.Hour() < 23 || cron.day == -1) {
			//rolled over, and hour is not specified, and day is either not specified or increment won't roll over, so add an hour
			nextRun = nextRun.Add(1 * time.Hour)
		}
	}

	// nextRun is still in the past, try advancing the hour if we're allowed

	if !nextRun.After(now) && cron.hour == -1 {
		// try the next hour
		v := now.Hour() + 1
		nextRun = time.Date(nextRun.Year(), nextRun.Month(), nextRun.Day(), 
			v%24, 
			Iif(cron.min > -1, cron.min, 0), // use minute if specified, else the start of the next hour
			0, 0, now.Location(),
		)
		if v >= 24 && cron.day == -1 {
			//rolled over, add a day
			n := nextRun.AddDate(0, 0, 1)
			if n.Day() > 1 || cron.mon == -1 {
				// either month didn't change, or wasn't pinned, so keep
				nextRun = n
			}
		}
	}

	if !nextRun.After(now) && cron.day == -1 {
		// try the next day
		n := nextRun.AddDate(0, 0, 1)
		if n.Day() > 1 || cron.mon == -1 {
			// either month didn't change, or wasn't pinned
			nextRun = n
			nextRun = time.Date(nextRun.Year(), 
				time.Month(Iif(cron.mon > -1, cron.mon, int(nextRun.Month()))), 
				nextRun.Day(), 
				Iif(cron.hour > -1, cron.hour, 0),  // use hour if specified, else the start of the next day
				Iif(cron.min > -1, cron.min, 0), // use minute if specified, else the start of the next hour
				0, 0, now.Location(),
			)
		}
	}

	if !nextRun.After(now) && cron.mon == -1 {
		// try the next month
		nextRun = nextRun.AddDate(0, 1, 0)
		nextRun = time.Date(nextRun.Year(), nextRun.Month(), 
			Iif(cron.day > -1, cron.day, 1),  // use day if specified, else the start of the next month
			Iif(cron.hour > -1, cron.hour, 0),  // use hour if specified, else the start of the next day
			Iif(cron.min > -1, cron.min, 0), // use minute if specified, else the start of the next hour
			0, 0, now.Location(),
		)
	}

	if !nextRun.After(now) {
		// try the next year
		nextRun = time.Date(
			nextRun.Year()+1, 
			time.Month(Iif(cron.mon > -1, cron.mon, 1)), 
			Iif(cron.day > -1, cron.day, 1),  // use day if specified, else the start of the next month
			Iif(cron.hour > -1, cron.hour, 0),  // use hour if specified, else the start of the next day
			Iif(cron.min > -1, cron.min, 0), // use minute if specified, else the start of the next hour
			0, 0, now.Location(),
		)
	}

    // if a weekday is specified, and nextRun isn't on it (or is in the past)

	if cron.wday != -1 && (cron.wday != int(nextRun.Weekday()) || !nextRun.After(now)) {
		daysToAdd := cron.wday - int(nextRun.Weekday())

		if daysToAdd <= 0 {
			daysToAdd += 7
		}

		nextRun = nextRun.AddDate(0, 0, daysToAdd)
	}

	return nextRun
}
