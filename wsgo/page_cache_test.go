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
	rec := httptest.NewRecorder()
	cw := NewCacheWriter(rec)
	cw.Header().Set("Cache-Control", "max-age=60")
	cw.WriteHeader(200)
	cw.Write([]byte(body))
	CachePage(cw, req)
}

// TryCache used to read entries after releasing pageCacheMutex, while
// CachePage replaced slots in the same backing array under the lock. Run with
// -race to detect that.
func TestPageCacheConcurrentAccess(t *testing.T) {
	oldMaxAge, oldMaxAgeBeforeRefetch, oldLimit := maxAge, maxAgeBeforeRefetch, pageCacheLimit
	defer func() {
		maxAge, maxAgeBeforeRefetch, pageCacheLimit = oldMaxAge, oldMaxAgeBeforeRefetch, oldLimit
		resetPageCacheForTest()
	}()
	maxAge = 3600
	maxAgeBeforeRefetch = 0
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
	oldMaxAge, oldMaxAgeBeforeRefetch, oldLimit := maxAge, maxAgeBeforeRefetch, pageCacheLimit
	defer func() {
		maxAge, maxAgeBeforeRefetch, pageCacheLimit = oldMaxAge, oldMaxAgeBeforeRefetch, oldLimit
		resetPageCacheForTest()
	}()
	maxAge = 3600
	maxAgeBeforeRefetch = 0
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
