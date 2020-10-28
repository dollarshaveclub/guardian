package guardian

import (
	"context"
	"fmt"
	"math"
	"strconv"
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

	// Incr increments key by count and sets the expiration to expireIn from now. The result of the incr, whether to force block,
	// and an error is returned
	Incr(context context.Context, key string, incryBy uint, maxBeforeBlock uint64, expireIn time.Duration) (uint64, bool, error)
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

// Limit will recommend to block a request if the request exceeds the rate limit.
// Limit is intended to be one of many function calls made to determine if a request should be blocked.
// So we indicate in the response whether the request should be blocked and if the caller should continue processing this request.
func (rl *GenericRateLimiter) Limit(context context.Context, request Request) (RequestBlockerResp, uint32, error) {
	if rl.KeyFunc == nil || rl.LimitProvider == nil || rl.Counter == nil || rl.Logger == nil {
		return AllowedStop, math.MaxUint32, nil
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
		// we didn't find a matching limit for this request, so we should allow this but continue processing
		return AllowedContinue, math.MaxUint32, nil
	}

	key := SlotKey(rl.KeyFunc(request), time.Now().UTC(), limit.Duration)
	rl.Logger.Debugf("generated key %v for request %v", key, request)

	currCount, blocked, err := rl.Counter.Incr(context, key, 1, limit.Count, limit.Duration)
	if err != nil {
		err = errors.Wrap(err, fmt.Sprintf("error incrementing counter for request %v", request))
		rl.Logger.WithError(err).Error("counter returned error when call incr")
		return AllowedStop, 0, err
	}

	ratelimited = blocked || currCount > limit.Count
	if ratelimited {
		rl.Logger.Debugf("request %v blocked", request)
		return BlockedStop, 0, err // block request, rate limited
	}

	remaining64 := limit.Count - currCount
	remaining32 := uint32(remaining64)
	if uint64(remaining32) != remaining64 { // if we lose some significant bits, convert it to max of uint32
		rl.Logger.Errorf("overflow detected, setting to max uint32: remaining64 %v remaining32", remaining64, remaining32)
		remaining32 = math.MaxUint32
	}

	// the request has been allowed, but since we have found a corresponding limit we should stop processing the request
	rl.Logger.Debugf("request %v allowed with %v remaining requests", request, remaining32)
	return AllowedStop, remaining32, err
}

// SlotKey generates the key for a slot determined by the request, slot time, and limit duration
func SlotKey(keybase string, slotTime time.Time, duration time.Duration) string {
	// a) convert to seconds
	// b) get slot time unix epoch seconds
	// c) use integer division to bucket based on limit.Duration
	// if secs = 10
	// 1522895020 -> 1522895020
	// 1522895021 -> 1522895020
	// 1522895028 -> 1522895020
	// 1522895030 -> 1522895030
	secs := int64(duration / time.Second) // a
	t := slotTime.Unix()                  // b
	slot := (t / secs) * secs             // c
	return keybase + ":" + strconv.FormatInt(slot, 10)
}
