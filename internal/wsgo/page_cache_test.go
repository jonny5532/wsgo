package wsgo

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func resetPageCacheForTest() {
	pageCacheMutex.Lock()
	pageCache = make(map[string][]*PageCacheEntry)
	pageCacheSize = 0
	pageCacheMutex.Unlock()
}

func cacheTestPage(req *http.Request, body string) {
	cacheTestPageWithHeaders(req, body, http.Header{"Cache-Control": {"max-age=60"}})
}

func cacheTestPageWithHeaders(req *http.Request, body string, header http.Header) bool {
	rec := httptest.NewRecorder()
	cw := NewCacheWriter(rec)
	for k, vv := range header {
		cw.Header()[k] = vv
	}
	cw.WriteHeader(200)
	cw.Write([]byte(body))
	return CachePage(cw, req)
}

// setUpPageCacheTest enables the page cache with the given --max-age, and
// restores the previous configuration when the test finishes.
func setUpPageCacheTest(t *testing.T, age int) {
	oldMaxAge, oldLimit := maxAge, pageCacheLimit
	t.Cleanup(func() {
		maxAge, pageCacheLimit = oldMaxAge, oldLimit
		resetPageCacheForTest()
		pageCacheMutex.Lock()
		cacheFills = make(map[string]*CacheFill)
		pageCacheMutex.Unlock()
	})
	maxAge = age
	pageCacheLimit = 1 << 30
	resetPageCacheForTest()
}

func newTestRequest(t *testing.T, url string, header http.Header) *http.Request {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, vv := range header {
		req.Header[k] = vv
	}
	return req
}

// onlyCacheEntry returns the single entry cached for a request.
func onlyCacheEntry(t *testing.T, req *http.Request) *PageCacheEntry {
	pageCacheMutex.Lock()
	defer pageCacheMutex.Unlock()
	entries := pageCache[DeriveCacheKey(req)]
	if len(entries) != 1 {
		t.Fatalf("expected one cache entry, got %d", len(entries))
	}
	return entries[0]
}

// ageCacheEntry moves an entry's timestamps back, as if it had been cached d
// ago.
func ageCacheEntry(entry *PageCacheEntry, d time.Duration) {
	pageCacheMutex.Lock()
	defer pageCacheMutex.Unlock()
	entry.created = entry.created.Add(-d)
	entry.refreshAfter = entry.refreshAfter.Add(-d)
	entry.expiry = entry.expiry.Add(-d)
	entry.staleUntil = entry.staleUntil.Add(-d)
}

// TryCache used to read entries after releasing pageCacheMutex, while
// CachePage replaced slots in the same backing array under the lock. Run with
// -race to detect that.
func TestPageCacheConcurrentAccess(t *testing.T) {
	oldMaxAge, oldLimit := maxAge, pageCacheLimit
	defer func() {
		maxAge, pageCacheLimit = oldMaxAge, oldLimit
		resetPageCacheForTest()
	}()
	maxAge = 3600
	pageCacheLimit = 1 << 30
	resetPageCacheForTest()

	req, err := http.NewRequest("GET", "http://example.com/race?x=1", nil)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				cacheTestPage(req, "hello world")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				rec := httptest.NewRecorder()
				TryCache(rec, req)
			}
		}()
	}
	wg.Wait()
}

// The page cache size limit must actually evict entries, otherwise an
// attacker can fill it with unique cacheable URLs indefinitely.
func TestPageCacheEvictsOldestWhenOverLimit(t *testing.T) {
	oldMaxAge, oldLimit := maxAge, pageCacheLimit
	defer func() {
		maxAge, pageCacheLimit = oldMaxAge, oldLimit
		resetPageCacheForTest()
	}()
	maxAge = 3600
	pageCacheLimit = 25000 // room for roughly two 11KB entries
	resetPageCacheForTest()

	reqFor := func(id string) *http.Request {
		req, err := http.NewRequest("GET", "http://example.com/limit/?id="+id, nil)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	hit := func(req *http.Request) bool {
		rec := httptest.NewRecorder()
		return TryCache(rec, req) == HIT
	}

	body := strings.Repeat("a", 10000) // ~11KB per entry
	cacheTestPage(reqFor("0"), body)
	if !hit(reqFor("0")) {
		t.Fatal("expected first entry to be a hit")
	}

	for i := 1; i <= 3; i++ {
		cacheTestPage(reqFor(fmt.Sprint(i)), body)
		time.Sleep(time.Millisecond)
	}

	if hit(reqFor("0")) {
		t.Fatal("expected the oldest entry to have been evicted when over the cache size limit")
	}
}

func TestCacheControlParsing(t *testing.T) {
	setUpPageCacheTest(t, 3600)

	cases := []struct {
		cacheControl []string
		lifetime     time.Duration
	}{
		{[]string{"max-age=60"}, 60 * time.Second},
		{[]string{"public,max-age=60"}, 60 * time.Second},
		{[]string{"Public, Max-Age=60"}, 60 * time.Second},
		{[]string{"public", "max-age=60"}, 60 * time.Second},
		{[]string{"max-age=\"60\""}, 60 * time.Second},
		{[]string{"max-age=7200"}, 3600 * time.Second},
		{[]string{"max-age=60, s-maxage=300"}, 300 * time.Second},
		{[]string{"s-maxage=300"}, 300 * time.Second},
		{[]string{"max-age=60, s-maxage=0"}, 0},
		{[]string{"max-age=0"}, 0},
		{[]string{"max-age=60,no-cache"}, 0},
		{[]string{"max-age=60", "No-Store"}, 0},
		{[]string{"private,max-age=60"}, 0},
		{[]string{"max-age=junk"}, 0},
		{nil, 0},
	}

	for i, c := range cases {
		req := newTestRequest(t, fmt.Sprintf("http://example.com/parse?%d", i), nil)
		cached := cacheTestPageWithHeaders(req, "body", http.Header{"Cache-Control": c.cacheControl})
		if c.lifetime == 0 {
			if cached {
				t.Errorf("%q: expected not to be cached", c.cacheControl)
			}
			continue
		}
		if !cached {
			t.Errorf("%q: expected to be cached", c.cacheControl)
			continue
		}
		entry := onlyCacheEntry(t, req)
		if got := entry.expiry.Sub(entry.created); got != c.lifetime {
			t.Errorf("%q: expected lifetime %v, got %v", c.cacheControl, c.lifetime, got)
		}
	}
}

func TestVaryParsing(t *testing.T) {
	setUpPageCacheTest(t, 3600)

	req := newTestRequest(t, "http://example.com/vary", http.Header{"Accept-Language": {"en"}})
	cacheTestPageWithHeaders(req, "en", http.Header{
		"Cache-Control": {"max-age=60"},
		"Vary":          {"cookie,accept-language"},
	})

	if TryCache(httptest.NewRecorder(), req) != HIT {
		t.Error("expected a hit for the same request headers")
	}
	other := newTestRequest(t, "http://example.com/vary", http.Header{"Accept-Language": {"fr"}})
	if TryCache(httptest.NewRecorder(), other) != MISS {
		t.Error("expected a miss for a different Accept-Language")
	}
	withCookie := newTestRequest(t, "http://example.com/vary", http.Header{"Accept-Language": {"en"}, "Cookie": {"a=b"}})
	if TryCache(httptest.NewRecorder(), withCookie) != MISS {
		t.Error("expected a miss for a different Cookie")
	}
}

func TestVaryStarIsNotCached(t *testing.T) {
	setUpPageCacheTest(t, 3600)

	req := newTestRequest(t, "http://example.com/star", nil)
	if cacheTestPageWithHeaders(req, "body", http.Header{"Cache-Control": {"max-age=60"}, "Vary": {"Accept, *"}}) {
		t.Fatal("expected a Vary: * response not to be cached")
	}
}

func TestVaryCookiesFilter(t *testing.T) {
	setUpPageCacheTest(t, 3600)

	req := newTestRequest(t, "http://example.com/cookies", http.Header{"Cookie": {"sessionid=1;tracker=x"}})
	cacheTestPageWithHeaders(req, "body", http.Header{
		"Cache-Control":       {"max-age=60"},
		"Vary":                {"Cookie"},
		"X-Wsgo-Vary-Cookies": {"sessionid,remember_me"},
	})

	sameSession := newTestRequest(t, "http://example.com/cookies", http.Header{"Cookie": {"tracker=y; sessionid=1"}})
	if TryCache(httptest.NewRecorder(), sameSession) != HIT {
		t.Error("expected a hit when only an ignored cookie differs")
	}
	otherSession := newTestRequest(t, "http://example.com/cookies", http.Header{"Cookie": {"sessionid=2"}})
	if TryCache(httptest.NewRecorder(), otherSession) != MISS {
		t.Error("expected a miss for a different session cookie")
	}
}

func TestCacheHitHeaders(t *testing.T) {
	setUpPageCacheTest(t, 3600)

	req := newTestRequest(t, "http://example.com/age", nil)
	cacheTestPage(req, "hello")
	ageCacheEntry(onlyCacheEntry(t, req), 10*time.Second)

	rec := httptest.NewRecorder()
	TryCache(rec, req)
	if got := rec.Header().Get("Age"); got != "10" {
		t.Errorf("expected Age: 10, got %q", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "5" {
		t.Errorf("expected Content-Length: 5, got %q", got)
	}

	// The stored headers mustn't have been modified.
	if onlyCacheEntry(t, req).header.Get("Age") != "" {
		t.Error("Age header leaked into the cache entry")
	}
}

// The refresh window used to be derived from --max-age rather than each
// entry's own lifetime, so a short-lived entry was refreshed on every hit.
func TestCacheRefreshWindowIsPerEntry(t *testing.T) {
	setUpPageCacheTest(t, 3600)

	req := newTestRequest(t, "http://example.com/refresh", nil)
	cacheTestPage(req, "body") // max-age=60
	entry := onlyCacheEntry(t, req)

	if got := TryCache(httptest.NewRecorder(), req); got != HIT {
		t.Fatalf("expected a plain hit for a new entry, got %v", got)
	}

	ageCacheEntry(entry, 46*time.Second)
	if got := TryCache(httptest.NewRecorder(), req); got != HIT_NEEDS_REFRESH {
		t.Fatalf("expected a refresh in the last quarter of the lifetime, got %v", got)
	}

	// Only one request should be told to refresh.
	for i := 0; i < 3; i++ {
		if got := TryCache(httptest.NewRecorder(), req); got != HIT {
			t.Fatalf("expected the refresh not to be repeated, got %v", got)
		}
	}

	// Unless the refresh didn't replace the entry within the request timeout.
	pageCacheMutex.Lock()
	entry.refreshStarted = entry.refreshStarted.Add(-time.Duration(requestTimeout+1) * time.Second)
	pageCacheMutex.Unlock()
	if got := TryCache(httptest.NewRecorder(), req); got != HIT_NEEDS_REFRESH {
		t.Fatalf("expected the refresh to be retried, got %v", got)
	}

	ageCacheEntry(entry, 15*time.Second)
	if got := TryCache(httptest.NewRecorder(), req); got != MISS {
		t.Fatalf("expected an expired entry to miss, got %v", got)
	}
}

func TestStaleWhileRevalidate(t *testing.T) {
	setUpPageCacheTest(t, 3600)

	req := newTestRequest(t, "http://example.com/swr", nil)
	cacheTestPageWithHeaders(req, "body", http.Header{"Cache-Control": {"max-age=60, stale-while-revalidate=30"}})
	entry := onlyCacheEntry(t, req)

	ageCacheEntry(entry, 70*time.Second)
	if got := TryCache(httptest.NewRecorder(), req); got != HIT_NEEDS_REFRESH {
		t.Fatalf("expected a stale entry to be served and refreshed, got %v", got)
	}
	if got := TryCache(httptest.NewRecorder(), req); got != HIT {
		t.Fatalf("expected a stale entry to be served without another refresh, got %v", got)
	}

	ageCacheEntry(entry, 25*time.Second)
	if got := TryCache(httptest.NewRecorder(), req); got != MISS {
		t.Fatalf("expected a miss past the stale-while-revalidate period, got %v", got)
	}
}

func TestCacheFillCoalescesMisses(t *testing.T) {
	setUpPageCacheTest(t, 3600)

	req := newTestRequest(t, "http://example.com/fill", nil)

	leader, wait := JoinCacheFill(req)
	if leader == nil || wait != nil {
		t.Fatal("expected the first request to lead the fill")
	}
	follower, wait := JoinCacheFill(req)
	if follower != nil || wait == nil {
		t.Fatal("expected a second request to wait for the fill")
	}

	cacheTestPage(req, "body")
	leader.Finish(true)

	select {
	case <-wait:
	default:
		t.Fatal("expected the waiting request to be released")
	}
	if TryCache(httptest.NewRecorder(), req) != HIT {
		t.Fatal("expected the waiting request to find the response in the cache")
	}
}

func TestCacheFillSeparatesVaryingRequests(t *testing.T) {
	setUpPageCacheTest(t, 3600)

	userA := newTestRequest(t, "http://example.com/account", http.Header{"Cookie": {"sessionid=a"}})
	userB := newTestRequest(t, "http://example.com/account", http.Header{"Cookie": {"sessionid=b"}})
	cacheTestPageWithHeaders(userA, "a", http.Header{"Cache-Control": {"max-age=60"}, "Vary": {"Cookie"}})

	fillB, wait := JoinCacheFill(userB)
	if fillB == nil || wait != nil {
		t.Fatal("expected user B to lead their own fill")
	}
	defer fillB.Finish(false)

	userC := newTestRequest(t, "http://example.com/account", http.Header{"Cookie": {"sessionid=c"}})
	fillC, wait := JoinCacheFill(userC)
	if fillC == nil || wait != nil {
		t.Fatal("expected user C not to wait for user B's response")
	}
	fillC.Finish(false)
}

func TestCacheFillSkipsUncacheableURLs(t *testing.T) {
	setUpPageCacheTest(t, 3600)

	req := newTestRequest(t, "http://example.com/uncacheable", nil)

	leader, _ := JoinCacheFill(req)
	_, wait := JoinCacheFill(req)
	leader.Finish(false)
	<-wait

	if fill, wait := JoinCacheFill(req); fill != nil || wait != nil {
		t.Fatal("expected requests for an uncacheable URL not to be coalesced")
	}
	if TryCache(httptest.NewRecorder(), req) != MISS {
		t.Fatal("expected the uncacheable marker never to be served")
	}

	// Once the URL is cacheable again, its marker should be removed.
	cacheTestPage(req, "body")
	onlyCacheEntry(t, req)
	if fill, _ := JoinCacheFill(req); fill == nil {
		t.Fatal("expected coalescing to resume")
	} else {
		fill.Finish(true)
	}
}
