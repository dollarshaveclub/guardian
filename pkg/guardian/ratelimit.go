package guardian

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// Limit describes a rate limit
type Limit struct {
	Count    uint64
	Duration time.Duration
	Enabled  bool
}

func (l Limit) String() string {
	return fmt.Sprintf("Limit(%d per %v, enabled: %v)", l.Count, l.Duration, l.Enabled)
}

// LimitProvider provides the current limit settings
type LimitProvider interface {
	// GetLimit returns the current limit settings
	GetLimit() Limit
}

// Counter is a data store capable of incrementing and expiring the count of a key
type Limiter interface {
	Limit(context context.Context, request Request, limit Limit) (bool, uint32, error)
}

// NewIPRateLimiter creates a new IP rate limiter
func NewIPRateLimiter(conf LimitProvider, limiter Limiter, logger logrus.FieldLogger, reporter MetricReporter) *IPRateLimiter {
	return &IPRateLimiter{conf: conf, limiter: limiter, logger: logger, reporter: reporter}
}

// IPRateLimiter is an IP based rate limiter
type IPRateLimiter struct {
	conf     LimitProvider
	limiter  Limiter
	logger   logrus.FieldLogger
	reporter MetricReporter
}

// Limit limits a request if request exceeds rate limit
func (rl *IPRateLimiter) Limit(context context.Context, request Request) (bool, uint32, error) {
	start := time.Now()
	ratelimited := false
	var err error
	defer func() {
		rl.reporter.HandledRatelimit(request, ratelimited, err != nil, time.Now().Sub(start))
	}()

	limit := rl.conf.GetLimit()
	rl.logger.Debugf("fetched limit %v", limit)
	rl.reporter.CurrentLimit(limit)

	if !limit.Enabled {
		rl.logger.Debugf("limit not enabled for request %v, allowing", request)
		return false, ^uint32(0), nil
	}

	ratelimited, remaining, err := rl.limiter.Limit(context, request, limit)
	return ratelimited, remaining, err
}
