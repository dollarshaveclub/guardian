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

// LimitProvider provides the current limit settings
type LimitProvider interface {
	// GetLimit returns the current limit settings
	GetLimit() Limit
}

// Counter is a data store capable of incrementing and expiring the count of a key
type Counter interface {

	// Incr increments key by count and sets the expiration to expireIn from now. The result of the incr, whether to force block,
	// and an error is returned
	Incr(context context.Context, key string, incryBy uint, maxBeforeBlock uint64, expireIn time.Duration) (uint64, bool, error)
}

// GenericRateLimiter is a rate limiter that uses a function to decide the counter key for rate limiting
type GenericRateLimiter struct {
	KeyFunc  func(req Request) string
	Conf     LimitProvider
	Counter  Counter
	Logger   logrus.FieldLogger
	Reporter MetricReporter
}

// Limit limits a request if request exceeds rate limit
func (rl *GenericRateLimiter) Limit(context context.Context, request Request) (bool, uint32, error) {
	if rl.KeyFunc == nil || rl.Conf == nil || rl.Counter == nil || rl.Logger == nil || rl.Reporter == nil {
		return false, math.MaxUint32, nil
	}

	start := time.Now().UTC()
	ratelimited := false
	var err error
	defer func() {
		rl.Reporter.HandledRatelimit(request, ratelimited, err != nil, time.Since(start))
	}()

	limit := rl.Conf.GetLimit()
	rl.Logger.Debugf("fetched limit %v", limit)
	rl.Reporter.CurrentLimit(limit)

	if !limit.Enabled {
		rl.Logger.Debugf("limit not enabled for request %v, allowing", request)
		return false, math.MaxUint32, nil
	}

	key := SlotKey(rl.KeyFunc(request), time.Now().UTC(), limit.Duration)
	rl.Logger.Debugf("generated key %v for request %v", key, request)

	currCount, blocked, err := rl.Counter.Incr(context, key, 1, limit.Count, limit.Duration)
	if err != nil {
		err = errors.Wrap(err, fmt.Sprintf("error incrementing limit for request %v", request))
		rl.Logger.WithError(err).Error("counter returned error when call incr")
		return false, 0, err
	}

	ratelimited = blocked || currCount > limit.Count
	if ratelimited {
		rl.Logger.Debugf("request %v blocked", request)
		return ratelimited, 0, err // block request, rate limited
	}

	remaining64 := limit.Count - currCount
	remaining32 := uint32(remaining64)
	if uint64(remaining32) != remaining64 { // if we lose some signifcant bits, convert it to max of uint32
		rl.Logger.Errorf("overflow detected, setting to max uint32: remaining64 %v remaining32", remaining64, remaining32)
		remaining32 = math.MaxUint32
	}

	rl.Logger.Debugf("request %v allowed with %v remaining requests", request, remaining32)
	return ratelimited, remaining32, err
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
