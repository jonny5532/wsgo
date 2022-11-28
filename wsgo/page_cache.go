package wsgo

import (
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type PageCacheEntry struct {
	buf         []byte
	statusCode  int
	header      http.Header
	vary        map[string]*string
	varyCookies string
	expiry      time.Time
	hits        uint64
}

var pageCache map[string][]*PageCacheEntry
var pageCacheMutex sync.Mutex
var pageCacheSize uint64

func init() {
	pageCache = make(map[string][]*PageCacheEntry)
}

// TODO - add an occasional routine to free up expired entries

type TryCacheResponse int

const (
	MISS                  TryCacheResponse = 0
	HIT                                    = 1
	HIT_BUT_EXPIRING_SOON                  = 2
)

func DeriveCacheKey(req *http.Request) string {
	return req.Host + req.URL.Path + "?" + req.URL.RawQuery
}

func TryCache(w http.ResponseWriter, req *http.Request) TryCacheResponse {
	cacheKey := DeriveCacheKey(req)
	now := time.Now()

	pageCacheMutex.Lock()
	entries := pageCache[cacheKey]
	pageCacheMutex.Unlock()

	for _, cached := range entries {
		if cached.statusCode > 0 {
			// cache entry is valid

			if now.After(cached.expiry) {
				// has expired
				continue
			}

			varies := false
			for k, v := range cached.vary {
				req_val := req.Header.Get(k)
				if k == "Cookie" {
					// strip out irrelevant cookies
					req_val = ExtractCookies(req_val, cached.varyCookies)
				}

				if req_val != *v {
					varies = true
					break
				}
			}
			if varies {
				// varies on a Vary header so can't use this cache
				continue
			}

			for k, vv := range cached.header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(cached.statusCode)
			w.Write(cached.buf)

			atomic.AddUint64(&cached.hits, 1)

			if cached.expiry.Sub(now) < (time.Duration(maxAgeBeforeRefetch) * time.Second) {
				return HIT_BUT_EXPIRING_SOON
			}

			return HIT
		}
	}
	return MISS
}

func ExtractCookies(cookies string, filter string) string {
	/*
		Filter a supplied cookie header (" ;" delimited) and filter list (" ,"
		delimited cookie names), returning the new cookie header.

		Only cookies with names in the filter list will be included in the response.

		If the filter is blank, all cookies are returned.

	*/

	if filter == "" {
		//no filter, default to all cookies
		return cookies
	}

	var ret []string

	cookie_map := make(map[string]string)
	for _, cookie := range strings.Split(cookies, "; ") {
		cookie_bits := strings.SplitN(cookie, "=", 2)
		if len(cookie_bits) < 2 || cookie_bits[0] == "" {
			// nonsensical cookie, keep it
			ret = append(ret, cookie)
		} else {
			cookie_map[cookie_bits[0]] = cookie_bits[1]
		}
	}

	for _, f := range strings.Split(filter, ", ") {
		if f == "" {
			continue
		}
		ret = append(ret, f+"="+cookie_map[f])
	}

	return strings.Join(ret, "; ")
}

func CachePage(cw *CacheWriter, req *http.Request) {
	responseHeaders := cw.Header()

	if req.Header.Get("Authorization") != "" {
		return
	}

	lifetimeSeconds := maxAge
	for _, cc := range strings.Split(responseHeaders.Get("Cache-Control"), ", ") {
		if cc == "no-cache" || cc == "no-store" || cc == "private" || cc == "max-age=0" {
			return
		} else if len(cc) > 8 && cc[:8] == "max-age=" {
			n, err := strconv.Atoi(cc[8:len(cc)])
			if err != nil {
				lifetimeSeconds = min(lifetimeSeconds, n)
			}
		}
	}

	// Store any request headers which are indicated by the Vary header, as the
	// cache will need to be keyed on these.
	varies := make(map[string]*string)
	for _, vary := range strings.Split(responseHeaders.Get("Vary"), ", ") {
		if vary == "" {
			continue
		}

		header := req.Header.Get(vary)
		varies[vary] = &header
	}

	// If the server sent the X-WSGo-Vary-Cookies header, then filter the
	// cookie key to just the request cookies identified in that.
	varyCookies := responseHeaders.Get("X-WSGo-Vary-Cookies")
	if varies["Cookie"] != nil && varyCookies != "" {
		v := ExtractCookies(*varies["Cookie"], varyCookies)
		varies["Cookie"] = &v
	}

	if responseHeaders.Get("Set-Cookie") != "" {
		// don't cache set-cookie response
		return
	}

	// TODO: facility for stripping uninteresting params from query (eg utm_ trackers and stuff)

	cacheKey := DeriveCacheKey(req)
	entry := &PageCacheEntry{
		buf:         cw.buf,
		statusCode:  cw.statusCode,
		header:      responseHeaders,
		vary:        varies,
		varyCookies: varyCookies,
		expiry:      time.Now().Add(time.Second * time.Duration(lifetimeSeconds)),
	}

	if entry.Size() > pageCacheLimit {
		return
	}

	if pageCacheSize > pageCacheLimit { //64mb
		PruneCache()
	}

	reusing_slot := false
	pageCacheMutex.Lock()
	entries := pageCache[cacheKey]
	for i, existing := range entries {
		if reflect.DeepEqual(existing.vary, entry.vary) && existing.varyCookies == entry.varyCookies {
			pageCacheSize += -entries[i].Size() + entry.Size()
			entries[i] = entry
			reusing_slot = true
			break
		}
	}

	if !reusing_slot {
		pageCache[cacheKey] = append(pageCache[cacheKey], entry)
		pageCacheSize += entry.Size()
	}
	pageCacheMutex.Unlock()
}

func (entry *PageCacheEntry) Size() uint64 {
	//very crude guess at entry size in memory
	return 1000 + uint64(len(entry.buf))
}

func PruneCache() {
	pageCacheMutex.Lock()
	// todo, make this not lock the map for the entire time
	fmt.Println("Pruning page cache, current size", pageCacheSize)

	now := time.Now()
	keepCache := func(entry *PageCacheEntry) bool {
		return entry.hits > 0 && entry.expiry.After(now)
	}

	for k, entries := range pageCache {
		skip := true
		for _, entry := range entries {
			if !keepCache(entry) {
				skip = false
			}
		}
		if skip {
			continue
		}

		var sizeChange uint64
		var newEntries []*PageCacheEntry
		for _, entry := range entries {
			if keepCache(entry) {
				sizeChange += entry.Size()
				newEntries = append(newEntries, entry)
			} else {
				sizeChange -= entry.Size()
			}
		}
		// overwrite entire entry slice
		pageCache[k] = newEntries
		pageCacheSize += sizeChange
	}
	fmt.Println("Page cache size now", pageCacheSize)
	pageCacheMutex.Unlock()
}
