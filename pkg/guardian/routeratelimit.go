package guardian

import (
	"github.com/sirupsen/logrus"
	"net/url"
	"time"
)

// This file contains the implementation of a Route Rate Limiter.
// This is achieved by writing custom implementations of the KeyFunc and LimitProvider that the GenericRateLimiter
// struct depends upon.

//RouteRateLimitStore provides the ability to retrieve rate limit configuration for a given route
type RouteRateLimitStore interface {
	GetRouteRateLimit(url url.URL) Limit
}

// RouteLimitProvider implements the LimitProvider interface.
type RouteLimitProvider struct {
	logger logrus.FieldLogger
	store RouteRateLimitStore
}

// GetLimit gets the limit for a particular request's path.
func (rlp *RouteLimitProvider) GetLimit(req Request) Limit {
	reqUrl, err := url.Parse(req.Path)
	if err != nil || reqUrl == nil {
		return Limit{Enabled: false}
	}

	rlp.logger.Debugf("Getting route rate limit for %v", reqUrl)
	return rlp.store.GetRouteRateLimit(*reqUrl)
}

// NewRouteRateLimitProvider returns an implementation of a LimitProvider intended to provide limits based off a
// a request's path and a client's IP address.
func NewRouteRateLimitProvider(store RouteRateLimitStore, logger logrus.FieldLogger) *RouteLimitProvider {
	return &RouteLimitProvider{store: store, logger: logger}
}

// RouteRateLimiterKeyFunc provides a key unique to a particular client IP and request path.
func RouteRateLimiterKeyFunc(req Request) string {
	return req.RemoteAddress + ":" + req.Path
}

func OnRouteRateLimitHandled(mr MetricReporter) RateLimitHook {
	return func(req Request, limit Limit, rateLimited bool, dur time.Duration, rlErr error) {
		if rateLimited {
			// Only report the route metadata when a request has been rate limited. This allows the cardinality of these
			// custom metrics to be bounded by the number of enabled route rate limits.
			mr.HandledRatelimitWithRoute(req, rateLimited, rlErr != nil, dur)
			return
		}
		mr.HandledRatelimit(req, rateLimited, rlErr != nil, dur)
	}
}

func NewRouteRateLimiter(store RouteRateLimitStore, logger logrus.FieldLogger, mr MetricReporter, c Counter) *GenericRateLimiter {
	return &GenericRateLimiter{
		KeyFunc:            RouteRateLimiterKeyFunc,
		LimitProvider:      NewRouteRateLimitProvider(store, logger),
		Counter:            c,
		Logger:             logger,
		OnRateLimitHandled: []RateLimitHook{OnRouteRateLimitHandled(mr)},
	}
}
