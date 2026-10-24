package wsgo

import (
	"fmt"
	"net/http"
	"net/url"
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
	statusCode  int // 0 for an uncacheable marker, which is never served
	header      http.Header
	vary        map[string]*string
	varyCookies string
	created     time.Time
	// Past refreshAfter, a hit also triggers a background refresh. The entry
	// is fresh until expiry, and can still be served whilst it is being
	// refreshed until staleUntil (from stale-while-revalidate).
	refreshAfter   time.Time
	expiry         time.Time
	staleUntil     time.Time
	refreshStarted time.Time
	lastUsed       time.Time
	hits           uint64
}

// The cached responses for a path, by query string.
type pathCache struct {
	// The query parameters to ignore for this path, from the application's
	// X-WSGo-Ignore-Params header.
	ignoreParams string
	filter       *paramFilter
	queries      map[string][]*PageCacheEntry
}

// Identifies where a request's responses are cached.
type cacheKey struct {
	path  string
	query string
}

var pageCache map[string]*pathCache
var pageCacheMutex sync.Mutex
var pageCacheSize uint64

// In-progress cache fills, by coalescing key. Protected by pageCacheMutex.
var cacheFills map[string]*CacheFill

func init() {
	pageCache = make(map[string]*pathCache)
	cacheFills = make(map[string]*CacheFill)
}

// TODO - add an occasional routine to free up expired entries

type TryCacheResponse int

const (
	MISS TryCacheResponse = iota
	HIT
	HIT_NEEDS_REFRESH
)

// How long a URL whose response turned out not to be cacheable is exempt from
// miss coalescing, so that requests for it don't queue up behind each other.
const uncacheableMarkerLifetime = 5 * time.Minute

func derivePathKey(req *http.Request) string {
	// The request method is part of the key so that a cached response can
	// never be served in response to a different method.
	return req.Method + " " + req.Host + req.URL.Path
}

// cacheKeyLocked returns the key a request's responses are cached under,
// leaving out any query parameters that the application has said to ignore
// for its path. pageCacheMutex must be held.
func cacheKeyLocked(req *http.Request) cacheKey {
	key := cacheKey{path: derivePathKey(req), query: req.URL.RawQuery}
	if pc := pageCache[key.path]; pc != nil {
		key.query = pc.filter.strip(key.query)
	}
	return key
}

// pageCacheMutex must be held.
func entriesLocked(key cacheKey) []*PageCacheEntry {
	if pc := pageCache[key.path]; pc != nil {
		return pc.queries[key.query]
	}
	return nil
}

// setEntriesLocked replaces the entries cached under a key, removing the key
// (and its path, once it has none left) if there are none.
// pageCacheMutex must be held.
func setEntriesLocked(key cacheKey, entries []*PageCacheEntry) {
	pc := pageCache[key.path]
	if pc == nil {
		if len(entries) == 0 {
			return
		}
		pc = &pathCache{queries: make(map[string][]*PageCacheEntry)}
		pageCache[key.path] = pc
	}
	if len(entries) > 0 {
		pc.queries[key.query] = entries
	} else {
		delete(pc.queries, key.query)
		if len(pc.queries) == 0 {
			delete(pageCache, key.path)
		}
	}
}

// A paramFilter matches query parameter names, either exactly or by a prefix
// (from a name ending in "*"), ignoring case.
type paramFilter struct {
	names    map[string]bool
	prefixes []string
}

// parseParamFilter parses a comma separated list of parameter names, returning
// nil if it is empty.
func parseParamFilter(list string) *paramFilter {
	var f *paramFilter
	for _, name := range splitHeaderList([]string{list}) {
		if f == nil {
			f = &paramFilter{names: make(map[string]bool)}
		}
		name = strings.ToLower(name)
		if prefix, ok := strings.CutSuffix(name, "*"); ok {
			f.prefixes = append(f.prefixes, prefix)
		} else {
			f.names[name] = true
		}
	}
	return f
}

func (f *paramFilter) matches(name string) bool {
	name = strings.ToLower(name)
	if f.names[name] {
		return true
	}
	for _, prefix := range f.prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// strip removes the matching parameters from a raw query string, leaving the
// others as they were. A nil filter strips nothing.
func (f *paramFilter) strip(rawQuery string) string {
	if f == nil || rawQuery == "" {
		return rawQuery
	}

	params := strings.Split(rawQuery, "&")
	kept := params[:0]
	for _, param := range params {
		name, _, _ := strings.Cut(param, "=")
		if unescaped, err := url.QueryUnescape(name); err == nil {
			name = unescaped
		}
		if !f.matches(name) {
			kept = append(kept, param)
		}
	}
	return strings.Join(kept, "&")
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
	now := time.Now()

	// The entry must be selected (and lastUsed/hits updated) whilst the lock
	// is held, otherwise a concurrent CachePage can replace the slot while we
	// are reading it. The fields needed to write the response are copied out
	// so the lock can be released before writing to the client.
	pageCacheMutex.Lock()
	var found *PageCacheEntry
	for _, cached := range entriesLocked(cacheKeyLocked(req)) {
		if cached.statusCode <= 0 || !now.Before(cached.staleUntil) {
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

	result := HIT
	// Only one request at a time refreshes an entry. If a refresh doesn't
	// replace the entry (because the application errored, or the client went
	// away before it ran), another is allowed once it has had time to finish.
	if now.After(found.refreshAfter) && now.Sub(found.refreshStarted) > time.Duration(requestTimeout)*time.Second {
		found.refreshStarted = now
		result = HIT_NEEDS_REFRESH
	}

	statusCode := found.statusCode
	header := found.header
	buf := found.buf
	created := found.created
	pageCacheMutex.Unlock()

	for k, vv := range header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Age", strconv.Itoa(int(now.Sub(created).Seconds())))
	if req.Method != "HEAD" && statusCode != 204 {
		// Lets the client see that the response is complete without waiting
		// for the handler to return, which it won't do until after any
		// refresh.
		w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	}
	w.WriteHeader(statusCode)
	w.Write(buf)

	if result == HIT_NEEDS_REFRESH {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	return result
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
	for _, cookie := range strings.Split(cookies, ";") {
		cookie = strings.TrimSpace(cookie)
		if cookie == "" {
			continue
		}
		cookie_bits := strings.SplitN(cookie, "=", 2)
		if len(cookie_bits) < 2 || cookie_bits[0] == "" {
			// nonsensical cookie, keep it
			ret = append(ret, cookie)
		} else {
			cookie_map[cookie_bits[0]] = cookie_bits[1]
		}
	}

	for _, f := range strings.Split(filter, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		ret = append(ret, f+"="+cookie_map[f])
	}

	return strings.Join(ret, "; ")
}

// splitHeaderList splits comma separated header values into their trimmed,
// non-empty items.
func splitHeaderList(values []string) []string {
	var items []string
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				items = append(items, item)
			}
		}
	}
	return items
}

// parseCacheControl maps the lowercased names of Cache-Control directives to
// their unquoted arguments.
func parseCacheControl(values []string) map[string]string {
	directives := make(map[string]string)
	for _, directive := range splitHeaderList(values) {
		name, arg, _ := strings.Cut(directive, "=")
		name = strings.ToLower(strings.TrimSpace(name))
		directives[name] = strings.Trim(strings.TrimSpace(arg), "\"")
	}
	return directives
}

// directiveSeconds returns the number of seconds given by a Cache-Control
// directive, or -1 if it is absent or invalid.
func directiveSeconds(directives map[string]string, name string) int {
	arg, ok := directives[name]
	if !ok {
		return -1
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 0 {
		return -1
	}
	return n
}

// requestMayBeCached reports whether the response to a request may be stored
// in the page cache.
func requestMayBeCached(req *http.Request) bool {
	if maxAge <= 0 || pageCacheLimit == 0 {
		return false
	}
	if req.Method != "GET" && req.Method != "HEAD" && req.Method != "OPTIONS" {
		return false
	}
	// Responses to authorised requests are likely to be specific to the user.
	return req.Header.Get("Authorization") == ""
}

// How a response's headers say it may be cached.
type cachePolicy struct {
	lifetime time.Duration
	// How long it may be served for after expiring, whilst being refreshed.
	staleWhileRevalidate time.Duration
	// The request headers it varies on.
	vary []string
}

// parseCachePolicy returns how a response may be cached, or false if it can't
// be.
func parseCachePolicy(statusCode int, header http.Header) (cachePolicy, bool) {
	var policy cachePolicy

	if statusCode != 200 && statusCode != 204 && statusCode != 301 {
		// Probably an error of some kind
		return policy, false
	}

	if header.Get("Set-Cookie") != "" {
		// don't cache set-cookie responses
		return policy, false
	}

	if contentLength, err := strconv.Atoi(header.Get("Content-Length")); err == nil && contentLength > cacheWriterLimit {
		return policy, false
	}

	directives := parseCacheControl(header.Values("Cache-Control"))
	for _, d := range []string{"no-cache", "no-store", "private"} {
		if _, ok := directives[d]; ok {
			return policy, false
		}
	}

	// s-maxage applies only to shared caches like this one, so it takes
	// precedence and allows a different lifetime to the one given to clients.
	lifetimeSeconds := directiveSeconds(directives, "s-maxage")
	if lifetimeSeconds < 0 {
		lifetimeSeconds = directiveSeconds(directives, "max-age")
	}
	lifetimeSeconds = min(maxAge, lifetimeSeconds)
	if lifetimeSeconds <= 0 {
		return policy, false
	}
	policy.lifetime = time.Duration(lifetimeSeconds) * time.Second

	staleSeconds := min(maxAge, max(0, directiveSeconds(directives, "stale-while-revalidate")))
	policy.staleWhileRevalidate = time.Duration(staleSeconds) * time.Second

	for _, vary := range splitHeaderList(header.Values("Vary")) {
		if vary == "*" {
			// varies on something other than request headers
			return policy, false
		}
		policy.vary = append(policy.vary, http.CanonicalHeaderKey(vary))
	}

	return policy, true
}

// CachePage stores the response written to cw, if its headers allowed it to
// be cached, returning whether it was.
func CachePage(cw *CacheWriter, req *http.Request) bool {
	if !cw.caching {
		return false
	}
	policy := cw.policy

	// Store any request headers which are indicated by the Vary header, as the
	// cache will need to be keyed on these.
	varies := make(map[string]*string)
	for _, vary := range policy.vary {
		header := req.Header.Get(vary)
		varies[vary] = &header
	}

	// If the server sent the X-WSGo-Vary-Cookies header, then filter the
	// cookie key to just the request cookies identified in that.
	if varies["Cookie"] != nil && cw.varyCookies != "" {
		v := ExtractCookies(*varies["Cookie"], cw.varyCookies)
		varies["Cookie"] = &v
	}

	buf := cw.buf
	if req.Method == "HEAD" {
		// The body of a HEAD response is never sent.
		buf = nil
	}

	now := time.Now()
	lifetime := policy.lifetime
	entry := &PageCacheEntry{
		buf:         buf,
		statusCode:  cw.statusCode,
		header:      cw.Header().Clone(),
		vary:        varies,
		varyCookies: cw.varyCookies,
		created:     now,
		// Refreshing during the last quarter of the lifetime gives a busy
		// page plenty of chances to be refreshed before it expires, whilst
		// only rendering it a third more often than if it were left to expire.
		refreshAfter: now.Add(lifetime - lifetime/4),
		expiry:       now.Add(lifetime),
		staleUntil:   now.Add(lifetime + policy.staleWhileRevalidate),
		lastUsed:     now,
	}

	if entry.Size() > pageCacheLimit {
		return false
	}

	pageCacheMutex.Lock()

	pathKey := derivePathKey(req)
	pc := pageCache[pathKey]
	if pc == nil {
		pc = &pathCache{queries: make(map[string][]*PageCacheEntry)}
		pageCache[pathKey] = pc
	}
	if pc.ignoreParams != cw.ignoreParams {
		// The path's existing entries were stored under keys made with the
		// old parameter list, so they can't be looked up reliably any more.
		for _, entries := range pc.queries {
			for _, existing := range entries {
				pageCacheSize -= existing.Size()
			}
		}
		pc.queries = make(map[string][]*PageCacheEntry)
		pc.ignoreParams = cw.ignoreParams
		pc.filter = parseParamFilter(cw.ignoreParams)
	}
	key := cacheKey{path: pathKey, query: pc.filter.strip(req.URL.RawQuery)}

	// Build a new slice rather than modifying the existing one in place.
	entries := pc.queries[key.query]
	updated := make([]*PageCacheEntry, 0, len(entries)+1)
	replaced := false
	for _, existing := range entries {
		if existing.statusCode <= 0 {
			// The URL turned out to be cacheable after all, so drop its
			// uncacheable marker.
			pageCacheSize -= existing.Size()
			continue
		}
		if !replaced && reflect.DeepEqual(existing.vary, entry.vary) && existing.varyCookies == entry.varyCookies {
			pageCacheSize -= existing.Size()
			updated = append(updated, entry)
			replaced = true
			continue
		}
		updated = append(updated, existing)
	}
	if !replaced {
		updated = append(updated, entry)
	}
	setEntriesLocked(key, updated)
	pageCacheSize += entry.Size()

	if pageCacheSize > pageCacheLimit {
		pruneCacheLocked()
	}

	pageCacheMutex.Unlock()
	return true
}

// A CacheFill lets concurrent requests which miss the cache wait for one of
// them to fetch the response from the application, instead of all running
// the application at once.
type CacheFill struct {
	key       string
	cacheKey  cacheKey
	done      chan struct{}
	followers int
}

// coalesceKeyLocked returns the key identifying requests that can share a
// response, or "" if the request shouldn't wait for another.
// pageCacheMutex must be held.
func coalesceKeyLocked(req *http.Request, ck cacheKey, now time.Time) string {
	key := ck.path + "?" + ck.query
	for _, entry := range entriesLocked(ck) {
		if entry.statusCode <= 0 {
			if now.Before(entry.staleUntil) {
				// Recently found to be uncacheable, so waiting would be
				// pointless.
				return ""
			}
			continue
		}

		// Assume the response will vary in the same way as existing entries,
		// so only wait for requests whose response could also be served to
		// this one (eg the same user, for pages which vary on cookies).
		names := make([]string, 0, len(entry.vary))
		for name := range entry.vary {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value := req.Header.Get(name)
			if name == "Cookie" {
				value = ExtractCookies(value, entry.varyCookies)
			}
			key += "\x00" + name + ":" + value
		}
		break
	}
	return key
}

// JoinCacheFill is called after a cache miss. If no other request is fetching
// the same response then this one becomes responsible for it, and must call
// Finish on the returned fill once its response has been handled. Otherwise
// it returns a channel which is closed when the other request has finished,
// after which the cache can be tried again. If the request shouldn't be
// coalesced, both are nil.
func JoinCacheFill(req *http.Request) (*CacheFill, <-chan struct{}) {
	pageCacheMutex.Lock()
	defer pageCacheMutex.Unlock()

	ck := cacheKeyLocked(req)
	key := coalesceKeyLocked(req, ck, time.Now())
	if key == "" {
		return nil, nil
	}
	if fill := cacheFills[key]; fill != nil {
		fill.followers++
		return nil, fill.done
	}

	fill := &CacheFill{
		key:      key,
		cacheKey: ck,
		done:     make(chan struct{}),
	}
	cacheFills[key] = fill
	return fill, nil
}

// Finish wakes any requests waiting for this fill. If some were waiting but
// the response wasn't cached, the URL is marked as uncacheable for a while so
// that later requests don't wait in vain.
func (fill *CacheFill) Finish(cached bool) {
	pageCacheMutex.Lock()
	delete(cacheFills, fill.key)
	if !cached && fill.followers > 0 {
		markUncacheableLocked(fill.cacheKey, time.Now())
	}
	pageCacheMutex.Unlock()

	close(fill.done)
}

// pageCacheMutex must be held.
func markUncacheableLocked(key cacheKey, now time.Time) {
	until := now.Add(uncacheableMarkerLifetime)
	entries := entriesLocked(key)
	for _, entry := range entries {
		if entry.statusCode <= 0 {
			entry.expiry = until
			entry.staleUntil = until
			entry.lastUsed = now
			return
		}
	}

	marker := &PageCacheEntry{
		created:    now,
		expiry:     until,
		staleUntil: until,
		lastUsed:   now,
	}
	updated := make([]*PageCacheEntry, 0, len(entries)+1)
	setEntriesLocked(key, append(append(updated, entries...), marker))
	pageCacheSize += marker.Size()

	if pageCacheSize > pageCacheLimit {
		pruneCacheLocked()
	}
}

func (entry *PageCacheEntry) Size() uint64 {
	//very crude guess at entry size in memory
	return 1000 + uint64(len(entry.buf))
}

// filterEntriesLocked keeps only the entries for which keep returns true,
// subtracting the size of the others from the cache size.
// pageCacheMutex must be held.
func filterEntriesLocked(keep func(*PageCacheEntry) bool) {
	for pathKey, pc := range pageCache {
		for query, entries := range pc.queries {
			kept := entries[:0]
			for _, entry := range entries {
				if keep(entry) {
					kept = append(kept, entry)
				} else {
					pageCacheSize -= entry.Size()
				}
			}
			setEntriesLocked(cacheKey{pathKey, query}, kept)
		}
	}
}

// pruneCacheLocked drops expired and never-served entries, and then evicts the
// least recently used entries until the cache is back under its limit.
// pageCacheMutex must be held.
func pruneCacheLocked() {
	// todo, make this not lock the map for the entire time
	fmt.Println("Pruning page cache, current size", pageCacheSize)

	now := time.Now()
	filterEntriesLocked(func(entry *PageCacheEntry) bool {
		return entry.staleUntil.After(now)
	})

	if pageCacheSize > pageCacheLimit {
		// Evict least recently used entries, leaving some headroom so that
		// pruning doesn't have to run on every subsequent insertion.
		target := pageCacheLimit - pageCacheLimit/10

		var all []*PageCacheEntry
		for _, pc := range pageCache {
			for _, entries := range pc.queries {
				all = append(all, entries...)
			}
		}
		sort.Slice(all, func(i, j int) bool {
			return all[i].lastUsed.After(all[j].lastUsed)
		})

		evicted := make(map[*PageCacheEntry]bool)
		size := pageCacheSize
		for size > target && len(all) > 0 {
			oldest := all[len(all)-1]
			size -= oldest.Size()
			evicted[oldest] = true
			all = all[:len(all)-1]
		}

		filterEntriesLocked(func(entry *PageCacheEntry) bool {
			return !evicted[entry]
		})
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
