package guardian

import (
	"github.com/sirupsen/logrus"
	"time"
)

// This file contains the implementation of an IP Rate Limiter.
// It enforces a limit that is applied upon every single request.
// This is achieved by writing custom implementations of the KeyFunc and LimitProvider that the GenericRateLimiter
// struct depends on.

// GlobalLimitProvider implements the LimitProvider interface.
type GlobalLimitProvider struct {
	*RedisConfStore
}

// GetLimit gets the global limit applied to all requests.
func (glp *GlobalLimitProvider) GetLimit(_ Request) Limit {
	glp.conf.RLock()
	defer glp.conf.RUnlock()

	return glp.conf.limit
}

// NewGlobalLimitProvider returns an implementation of a LimitProvider intended to provide limits for all requests from
// a given client IP.
func NewGlobalLimitProvider(rcs *RedisConfStore) *GlobalLimitProvider {
	return &GlobalLimitProvider{rcs}
}

// RouteRateLimiterKeyFunc provides a key unique to a particular client IP.
func IPRateLimiterKeyFunc(req Request) string {
	return req.RemoteAddress
}

// ReportRateLimitHandled reports that a rate limit request was handled and provides some metadata.
func ReportRateLimitHandled(mr MetricReporter) RateLimitHook {
	return func(req Request, limit Limit, rateLimited bool, dur time.Duration, rlErr error) {
		mr.HandledRatelimit(req, rateLimited, rlErr != nil, dur)
	}
}

// NewIPRateLimiter returns a rate limiter that enforces a limit upon all requests from each client IP.
func NewIPRateLimiter(rcf *RedisConfStore, l logrus.FieldLogger, mr MetricReporter, c Counter) *GenericRateLimiter {
	return &GenericRateLimiter{
		KeyFunc:            IPRateLimiterKeyFunc,
		LimitProvider:      NewGlobalLimitProvider(rcf),
		Counter:            c,
		Logger:             l,
		OnRateLimitHandled: []RateLimitHook{ReportRateLimitHandled(mr)},
	}
}
