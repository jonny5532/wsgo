package main

import (
	"sort"
)

type RollingAverage struct {
	samples  []int64
	position int
}

func NewRollingAverage() RollingAverage {
	return RollingAverage{
		samples: make([]int64, 0, 16),
	}
}

func (r *RollingAverage) Add(sample int64) {
	if len(r.samples) < 16 {
		r.samples = append(r.samples, sample)
	} else {
		r.samples[r.position] = sample
		r.position += 1
		if r.position >= 16 {
			r.position = 0
		}
	}
}

func (r *RollingAverage) GetFilteredMax() int64 {
	samples := append([]int64{}, r.samples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	ind := (3 * len(samples)) / 4 // take 75th percentile
	return samples[ind]
}
