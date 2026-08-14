package files

import (
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

type fileRateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *fileRateLimiter) allow(key string, max int, window time.Duration) bool {
	if max <= 0 || window <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-window)

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	times := l.hits[key]
	n := 0
	for _, ts := range times {
		if ts.After(cutoff) {
			times[n] = ts
			n++
		}
	}
	times = times[:n]
	if len(times) >= max {
		l.hits[key] = times
		return false
	}
	l.hits[key] = append(times, now)
	return true
}

func (f *Feature) checkCollectionFileRateLimit(e *core.RequestEvent, collection *core.Collection) error {
	if f == nil || e == nil || e.App == nil || collection == nil {
		return nil
	}
	settings := e.App.Settings()
	if settings == nil || !settings.RateLimits.Enabled || e.HasSuperuserAuth() {
		return nil
	}
	if ipInList(settings.RateLimits.ExcludedIPs, e.RealIP()) {
		return nil
	}

	labels := []string{collection.Name + ":file", "*:file"}
	audience := []string{core.RateLimitRuleAudienceAll, core.RateLimitRuleAudienceGuest}
	if e.Auth != nil {
		audience = []string{core.RateLimitRuleAudienceAll, core.RateLimitRuleAudienceAuth}
	}
	rule, ok := settings.RateLimits.FindRateLimitRule(labels, audience...)
	if !ok {
		return nil
	}

	key := e.RealIP()
	if key == "" {
		return nil
	}
	rtID := collection.Id + ":file:" + rule.Audience + ":" + key
	if !f.limiter.allow(rtID, rule.MaxRequests, time.Duration(rule.Duration)*time.Second) {
		return e.TooManyRequestsError("", nil)
	}
	return nil
}
