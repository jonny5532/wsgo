package wsgo

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	lastUsed    time.Time
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
	// The request method is part of the key so that a cached response can
	// never be served in response to a different method.
	return req.Method + " " + req.Host + req.URL.Path + "?" + req.URL.RawQuery
}

func (entry *PageCacheEntry) variesFrom(req *http.Request) bool {
	for k, v := range entry.vary {
		req_val := req.Header.Get(k)
		if k == "Cookie" {
			// strip out irrelevant cookies
			req_val = ExtractCookies(req_val, entry.varyCookies)
		}

		if req_val != *v {
			return true
		}
	}
	return false
}

func TryCache(w http.ResponseWriter, req *http.Request) TryCacheResponse {
	cacheKey := DeriveCacheKey(req)
	now := time.Now()

	// The entry must be selected (and lastUsed/hits updated) whilst the lock
	// is held, otherwise a concurrent CachePage can replace the slot while we
	// are reading it. The fields needed to write the response are copied out
	// so the lock can be released before writing to the client.
	pageCacheMutex.Lock()
	var found *PageCacheEntry
	for _, cached := range pageCache[cacheKey] {
		if cached.statusCode <= 0 || now.After(cached.expiry) {
			continue
		}
		if cached.variesFrom(req) {
			// varies on a Vary header so can't use this cache
			continue
		}
		found = cached
		break
	}
	if found == nil {
		pageCacheMutex.Unlock()
		return MISS
	}

	found.hits++
	found.lastUsed = now

	statusCode := found.statusCode
	header := found.header
	buf := found.buf
	expiringSoon := found.expiry.Sub(now) < (time.Duration(maxAgeBeforeRefetch) * time.Second)
	pageCacheMutex.Unlock()

	for k, vv := range header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(statusCode)
	w.Write(buf)

	if expiringSoon {
		return HIT_BUT_EXPIRING_SOON
	}

	return HIT
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

	lifetimeSeconds := 0
	for _, cc := range strings.Split(responseHeaders.Get("Cache-Control"), ", ") {
		if cc == "no-cache" || cc == "no-store" || cc == "private" || cc == "max-age=0" {
			return
		} else if len(cc) > 8 && cc[:8] == "max-age=" {
			n, err := strconv.Atoi(cc[8:len(cc)])
			if err == nil {
				lifetimeSeconds = min(maxAge, n)
			}
		}
	}

	if lifetimeSeconds <= 0 {
		return
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
		// don't cache set-cookie responses
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
		lastUsed:    time.Now(),
	}

	if entry.Size() > pageCacheLimit {
		return
	}

	pageCacheMutex.Lock()

	reusing_slot := false
	entries := pageCache[cacheKey]
	for i, existing := range entries {
		if reflect.DeepEqual(existing.vary, entry.vary) && existing.varyCookies == entry.varyCookies {
			pageCacheSize -= entries[i].Size()
			entries[i] = entry
			reusing_slot = true
			break
		}
	}

	if !reusing_slot {
		pageCache[cacheKey] = append(entries, entry)
	}
	pageCacheSize += entry.Size()

	if pageCacheSize > pageCacheLimit {
		pruneCacheLocked()
	}

	pageCacheMutex.Unlock()
}

func (entry *PageCacheEntry) Size() uint64 {
	//very crude guess at entry size in memory
	return 1000 + uint64(len(entry.buf))
}

// pruneCacheLocked drops expired and never-served entries, and then evicts the
// least recently used entries until the cache is back under its limit.
// pageCacheMutex must be held.
func pruneCacheLocked() {
	// todo, make this not lock the map for the entire time
	fmt.Println("Pruning page cache, current size", pageCacheSize)

	now := time.Now()

	type cacheEntry struct {
		key   string
		entry *PageCacheEntry
	}
	var all []cacheEntry

	for k, entries := range pageCache {
		kept := entries[:0]
		for _, entry := range entries {
			if entry.expiry.After(now) {
				kept = append(kept, entry)
				all = append(all, cacheEntry{k, entry})
			} else {
				pageCacheSize -= entry.Size()
			}
		}
		if len(kept) == 0 {
			delete(pageCache, k)
		} else {
			pageCache[k] = kept
		}
	}

	if pageCacheSize > pageCacheLimit {
		// Evict least recently used entries, leaving some headroom so that
		// pruning doesn't have to run on every subsequent insertion.
		target := pageCacheLimit - pageCacheLimit/10

		sort.Slice(all, func(i, j int) bool {
			return all[i].entry.lastUsed.After(all[j].entry.lastUsed)
		})

		for pageCacheSize > target && len(all) > 0 {
			oldest := all[len(all)-1]
			pageCacheSize -= oldest.entry.Size()
			all = all[:len(all)-1]
		}

		pageCache = make(map[string][]*PageCacheEntry)
		for _, item := range all {
			pageCache[item.key] = append(pageCache[item.key], item.entry)
		}
	}

	fmt.Println("Page cache size now", pageCacheSize)
}

// PruneCache immediately removes expired and unused entries. The cache is
// also pruned automatically whenever it grows beyond its configured size.
func PruneCache() {
	pageCacheMutex.Lock()
	pruneCacheLocked()
	pageCacheMutex.Unlock()
}
