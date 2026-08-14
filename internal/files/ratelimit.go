package files

import (
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const rateLimitCleanInterval = 30 * time.Minute

type rateHits struct {
	times  []time.Time
	window time.Duration
}

type fileRateLimiter struct {
	mu        sync.Mutex
	hits      map[string]*rateHits
	lastClean time.Time
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
		l.hits = map[string]*rateHits{}
	}
	b := l.hits[key]
	if b == nil {
		b = &rateHits{}
		l.hits[key] = b
	}
	b.window = window
	n := 0
	for _, ts := range b.times {
		if ts.After(cutoff) {
			b.times[n] = ts
			n++
		}
	}
	b.times = b.times[:n]
	allowed := len(b.times) < max
	if allowed {
		b.times = append(b.times, now)
	}
	l.maybeCleanLocked(now)
	return allowed
}

func (l *fileRateLimiter) clean(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanLocked(now)
}

func (l *fileRateLimiter) maybeCleanLocked(now time.Time) {
	if !l.lastClean.IsZero() && now.Sub(l.lastClean) < rateLimitCleanInterval {
		return
	}
	l.cleanLocked(now)
}

func (l *fileRateLimiter) cleanLocked(now time.Time) {
	l.lastClean = now
	for k, b := range l.hits {
		if b == nil || b.window <= 0 {
			delete(l.hits, k)
			continue
		}
		cutoff := now.Add(-b.window)
		n := 0
		for _, ts := range b.times {
			if ts.After(cutoff) {
				b.times[n] = ts
				n++
			}
		}
		if n == 0 {
			delete(l.hits, k)
			continue
		}
		b.times = b.times[:n]
	}
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
