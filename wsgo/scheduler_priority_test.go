package wsgo

import (
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func newPriorityTestJob(remoteAddr, userAgent, query string) *RequestJob {
	target := "http://example.com/"
	if query != "" {
		target += "?" + query
	}
	req := httptest.NewRequest("GET", target, nil)
	req.RemoteAddr = remoteAddr
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	return &RequestJob{req: req}
}

func TestGetRateLimitKey(t *testing.T) {
	cases := []struct {
		ip   string
		want string
	}{
		{"1.2.3.4", "1.2.3.4"},
		{"2001:db8:1234:5678:9abc:def0:1111:2222", "2001:db8:1234:5678::"},
		{"::1", "::"},
	}
	for _, c := range cases {
		if got := GetRateLimitKey(net.ParseIP(c.ip)); got != c.want {
			t.Errorf("GetRateLimitKey(%s) = %q, want %q", c.ip, got, c.want)
		}
	}
}

func TestAgeRequestCount(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		count float64
		age   time.Duration
		want  float64
	}{
		{"decays one per second", 10, 3 * time.Second, 7},
		{"clamps at zero", 2, 10 * time.Second, 0},
		{"zero stays zero", 0, 10 * time.Second, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := RequestCount{count: c.count, since: now.Add(-c.age)}
			if got := ageRequestCount(r, now); got != c.want {
				t.Errorf("ageRequestCount() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestAgedRequestCountRoundTrip(t *testing.T) {
	s := NewScheduler()
	key := "1.2.3.4"
	now := time.Now()

	if got := s.GetAgedRequestCount(key, now); got != 0 {
		t.Fatalf("empty count = %v, want 0", got)
	}

	s.AddAgedRequestCount(key, now)
	if got := s.GetAgedRequestCount(key, now); got != 1 {
		t.Fatalf("count after add = %v, want 1", got)
	}

	if got := s.GetAgedRequestCount(key, now.Add(time.Hour)); got != 0 {
		t.Fatalf("decayed count = %v, want 0", got)
	}
}

// TestAddAgedRequestCountConcurrent guards against the historic count being
// updated with a non-atomic read-modify-write, which silently loses increments.
func TestAddAgedRequestCountConcurrent(t *testing.T) {
	s := NewScheduler()
	key := "1.2.3.4"
	const n = 100

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.AddAgedRequestCount(key, time.Now())
		}()
	}
	wg.Wait()

	if got := s.GetAgedRequestCount(key, time.Now()); got < n-1 || got > n {
		t.Fatalf("concurrent count = %v, want ~%d (lost updates?)", got, n)
	}
}

func TestCalculateJobPriority(t *testing.T) {
	now := time.Now()
	rateKey := func(ip string) string { return GetRateLimitKey(net.ParseIP(ip)) }

	cases := []struct {
		name       string
		remoteAddr string
		userAgent  string
		query      string
		setup      func(*Scheduler, *RequestJob)
		want       int
	}{
		{name: "base", remoteAddr: "1.2.3.4:1", want: 1000},
		{name: "query string", remoteAddr: "1.2.3.4:1", query: "a=b", want: 500},
		{name: "crawler", remoteAddr: "1.2.3.4:1", userAgent: "Googlebot/2.1", want: -7000},
		{name: "crawler with query", remoteAddr: "1.2.3.4:1", userAgent: "bingbot", query: "a=b", want: -7500},
		{name: "loopback ignores crawler demotion", remoteAddr: "127.0.0.1:1", userAgent: "Googlebot", want: 1000},
		{
			name:       "active requests demote",
			remoteAddr: "1.2.3.4:1",
			setup: func(s *Scheduler, _ *RequestJob) {
				s.activeRequestsBySource[rateKey("1.2.3.4")] = 3
			},
			want: 1000 - 6000,
		},
		{
			name:       "historic requests demote",
			remoteAddr: "1.2.3.4:1",
			setup: func(s *Scheduler, _ *RequestJob) {
				s.AddAgedRequestCount(rateKey("1.2.3.4"), now)
				s.AddAgedRequestCount(rateKey("1.2.3.4"), now)
			},
			want: 1000 - 2000,
		},
		{
			name:       "historic count fully decayed",
			remoteAddr: "1.2.3.4:1",
			setup: func(s *Scheduler, _ *RequestJob) {
				s.AddAgedRequestCount(rateKey("1.2.3.4"), now.Add(-time.Hour))
			},
			want: 1000,
		},
		{
			name:       "forwarded public ip is rate limited",
			remoteAddr: "127.0.0.1:1",
			setup: func(s *Scheduler, j *RequestJob) {
				j.req.Header.Set("X-Forwarded-For", "8.8.8.8")
				s.activeRequestsBySource[rateKey("8.8.8.8")] = 1
			},
			want: 1000 - 2000,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewScheduler()
			job := newPriorityTestJob(c.remoteAddr, c.userAgent, c.query)
			if c.setup != nil {
				c.setup(s, job)
			}
			s.PrecomputeJobPriority(job)
			if got := s.CalculateJobPriority(job, now); got != c.want {
				t.Errorf("CalculateJobPriority() = %d, want %d", got, c.want)
			}
		})
	}
}

func TestPrecomputeJobPriorityFreezesStaticInputs(t *testing.T) {
	s := NewScheduler()
	job := newPriorityTestJob("1.2.3.4:1", "Googlebot/2.1", "")
	s.PrecomputeJobPriority(job)

	if job.basePriority != -7000 {
		t.Fatalf("basePriority = %d, want -7000", job.basePriority)
	}
	if job.rateLimitKey != "1.2.3.4" {
		t.Fatalf("rateLimitKey = %q, want 1.2.3.4", job.rateLimitKey)
	}
	if !job.demoteBySource {
		t.Fatal("demoteBySource = false, want true")
	}

	// The priority scan must use the precomputed values and not re-read the
	// request, so changing the request after queueing changes nothing.
	job.req.Header.Set("User-Agent", "Mozilla/5.0")
	if got := s.CalculateJobPriority(job, time.Now()); got != -7000 {
		t.Errorf("CalculateJobPriority() after request mutation = %d, want -7000", got)
	}
}

func TestGetHighestAndLowestPriorityJob(t *testing.T) {
	s := NewScheduler()
	high := newPriorityTestJob("1.2.3.4:1", "", "")
	query := newPriorityTestJob("1.2.3.4:2", "", "a=b")
	crawler := newPriorityTestJob("1.2.3.4:3", "bot", "")
	for _, job := range []*RequestJob{high, query, crawler} {
		s.PrecomputeJobPriority(job)
	}
	s.jobQueue = []*RequestJob{query, crawler, high}

	got, idx := s.GetHighestPriorityJob()
	if got != high || idx != 2 {
		t.Errorf("GetHighestPriorityJob() = index %d, want index 2", idx)
	}
	if high.priority != 1000 {
		t.Errorf("high priority = %d, want 1000", high.priority)
	}

	got, idx = s.GetLowestPriorityJob()
	if got != crawler || idx != 1 {
		t.Errorf("GetLowestPriorityJob() = index %d, want index 1", idx)
	}
	if crawler.priority != -7000 {
		t.Errorf("crawler priority = %d, want -7000", crawler.priority)
	}
}
