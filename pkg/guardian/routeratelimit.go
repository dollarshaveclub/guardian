package guardian

import (
	"github.com/sirupsen/logrus"
	"net/url"
	"time"
)

// This file contains the implementation of a Route Rate Limiter.
// This is achieved by writing custom implementations of the KeyFunc and LimitProvider that the GenericRateLimiter
// struct depends upon.

// RouteLimitProvider implements the LimitProvider interface.
type RouteLimitProvider struct {
	*RedisConfStore
}

// GetLimit gets the limit for a particular request's path.
func (rlp *RouteLimitProvider) GetLimit(req Request) Limit {
	reqUrl, err := url.Parse(req.Path)
	if err != nil || reqUrl == nil {
		rlp.logger.Warnf("unable to parse url from request: %v", err)
		return Limit{Enabled: false}
	}

	rlp.conf.RLock()
	defer rlp.conf.RUnlock()
	return rlp.conf.routeRateLimits[*reqUrl]
}

// NewRouteRateLimitProvider returns an implementation of a LimitProvider intended to provide limits based off a
// a request's path and a client's IP address.
func NewRouteRateLimitProvider(rcs *RedisConfStore) *RouteLimitProvider {
	return &RouteLimitProvider{rcs}
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

func NewRouteRateLimiter(rcf *RedisConfStore, l logrus.FieldLogger, mr MetricReporter, c Counter) *GenericRateLimiter {
	return &GenericRateLimiter{
		KeyFunc:            RouteRateLimiterKeyFunc,
		LimitProvider:      NewRouteRateLimitProvider(rcf),
		Counter:            c,
		Logger:             l,
		OnRateLimitHandled: []RateLimitHook{OnRouteRateLimitHandled(mr)},
	}
}
