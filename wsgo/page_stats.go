package wsgo

import (
	"sync"
)

type PageStat struct {
	responseTimes RollingAverage //in milliseconds
	cpuTimes RollingAverage
}

var pageStats map[string]*PageStat
var pageStatsMutex sync.Mutex

func init() {
	pageStats = make(map[string]*PageStat)
}

func RecordPageStats(url string, responseTimeMillis int64, cpuTimeMillis int64) {
	pageStatsMutex.Lock()
	var stat *PageStat
	if pageStats[url] != nil {
		stat = pageStats[url]
	} else {
		stat = &PageStat{
			responseTimes: NewRollingAverage(),
			cpuTimes: NewRollingAverage(),
		}
	}

	stat.responseTimes.Add(responseTimeMillis)
	stat.cpuTimes.Add(cpuTimeMillis)

	pageStats[url] = stat
	pageStatsMutex.Unlock()
}

func GetWeightedResponseTime(url string) int64 {
	pageStatsMutex.Lock()
	defer pageStatsMutex.Unlock()
	stat := pageStats[url]
	if stat == nil {
		//no time found
		return -1
	}

	return stat.responseTimes.GetFilteredMax()
}

func GetWeightedCpuTime(url string) int64 {
	pageStatsMutex.Lock()
	defer pageStatsMutex.Unlock()
	stat := pageStats[url]
	if stat == nil {
		//no time found
		return -1
	}

	return stat.cpuTimes.GetFilteredMax()
}
