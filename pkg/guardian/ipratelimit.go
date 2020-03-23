package guardian

import (
	"github.com/sirupsen/logrus"
	"time"
)

// This file contains the implementation of an IP Rate Limiter.
// It enforces a limit that is applied upon every single request.
// This is achieved by writing custom implementations of the KeyFunc and LimitProvider that the GenericRateLimiter
// struct depends on.

// GlobalLimitStore is a limit store that does not accept any request metadata but can still return a Limit.
type GlobalLimitStore interface {
	GetLimit() Limit
}

// GlobalLimitProvider implements the LimitProvider interface.
type GlobalLimitProvider struct {
	store GlobalLimitStore
}

// GetLimit gets the global limit applied to all requests.
func (glp *GlobalLimitProvider) GetLimit(_ Request) Limit {
	return glp.store.GetLimit()
}

// NewGlobalLimitProvider returns an implementation of a LimitProvider intended to provide limits for all requests from
// a given client IP.
func NewGlobalLimitProvider(store GlobalLimitStore) *GlobalLimitProvider {
	return &GlobalLimitProvider{store}
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
func NewIPRateLimiter(store GlobalLimitStore, l logrus.FieldLogger, mr MetricReporter, c Counter) *GenericRateLimiter {
	return &GenericRateLimiter{
		KeyFunc:            IPRateLimiterKeyFunc,
		LimitProvider:      NewGlobalLimitProvider(store),
		Counter:            c,
		Logger:             l,
		OnRateLimitHandled: []RateLimitHook{ReportRateLimitHandled(mr)},
	}
}
