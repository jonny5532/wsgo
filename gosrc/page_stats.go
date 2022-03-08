package main

import (
	"sync"
)

type PageStat struct {
	responseTimes RollingAverage //in milliseconds
}

var pageStats map[string]*PageStat
var pageStatsMutex sync.Mutex

func init() {
	pageStats = make(map[string]*PageStat)
}

func RecordPageStats(url string, timeMillis int64) {
	pageStatsMutex.Lock()
	var stat *PageStat
	if pageStats[url] != nil {
		stat = pageStats[url]
	} else {
		stat = &PageStat{
			responseTimes: NewRollingAverage(),
		}
	}

	stat.responseTimes.Add(timeMillis)

	pageStats[url] = stat
	pageStatsMutex.Unlock()
}

func GetWeightedLoadTime(url string) int64 {
	pageStatsMutex.Lock()
	defer pageStatsMutex.Unlock()
	stat := pageStats[url]
	if stat == nil {
		//no time found
		return -1
	}

	return stat.responseTimes.GetFilteredMax()
}
