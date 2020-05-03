package guardian

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/pkg/errors"
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

// LimitProvider provides the current limit settings based off a given request.
type LimitProvider interface {
	// GetLimit can determine what the limit is based off the data provided in the request.
	// It is up to the limit provider to determine the characteristics of the requests it cares about.
	// For example, a simple IP rate limiter could ignore the request entirely.
	GetLimit(req Request) Limit
}

// Counter is a data store capable of incrementing and expiring the count of a key
type Counter interface {
	Incr(context context.Context, incrBy uint, keyBase string, limit Limit) (uint64, error)
}

type RateLimitHook func(req Request, limit Limit, rateLimited bool, dur time.Duration, err error)

// GenericRateLimiter is a multipurpose rate limiter. It allows users to customize how the rate limiter behaves through 2 main mechanisms.
// 1. A KeyFunc that determines the key that wil be used for incrementing.
// 2. A LimitProvider that determines how the limit will be calculated.
type GenericRateLimiter struct {
	KeyFunc            func(req Request) string
	LimitProvider      LimitProvider
	Counter            Counter
	Logger             logrus.FieldLogger
	OnRateLimitHandled []RateLimitHook
}

// Limit limits a request if request exceeds rate limit
func (rl *GenericRateLimiter) Limit(context context.Context, request Request) (bool, uint32, error) {
	if rl.KeyFunc == nil || rl.LimitProvider == nil || rl.Counter == nil || rl.Logger == nil {
		return false, math.MaxUint32, nil
	}

	start := time.Now().UTC()
	ratelimited := false
	var limit Limit
	var err error
	defer func() {
		for _, hook := range rl.OnRateLimitHandled {
			hook(request, limit, ratelimited, time.Since(start), err)
		}

	}()

	limit = rl.LimitProvider.GetLimit(request)
	rl.Logger.Debugf("fetched limit %v for request %v", limit, request.Path)
	if !limit.Enabled {
		rl.Logger.Debugf("limit not enabled for request %v, allowing", request)
		return false, math.MaxUint32, nil
	}

	currCount, err := rl.Counter.Incr(context, 1, rl.KeyFunc(request), limit)
	if err != nil {
		err = errors.Wrap(err, fmt.Sprintf("error incrementing counter for request %v", request))
		rl.Logger.WithError(err).Error("counter returned error when call incr")
		return false, 0, err
	}

	ratelimited = currCount > limit.Count
	if ratelimited {
		rl.Logger.Debugf("request %v blocked", request)
		return ratelimited, 0, err // block request, rate limited
	}

	remaining64 := limit.Count - currCount
	remaining32 := uint32(remaining64)
	if uint64(remaining32) != remaining64 { // if we lose some significant bits, convert it to max of uint32
		rl.Logger.Errorf("overflow detected, setting to max uint32: remaining64 %v remaining32", remaining64, remaining32)
		remaining32 = math.MaxUint32
	}

	rl.Logger.Debugf("request %v allowed with %v remaining requests", request, remaining32)
	return ratelimited, remaining32, err
}
