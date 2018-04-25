package guardian

import (
	"context"
	"fmt"
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

	// Incr increments key by count and sets the expiration to expireIn from now. The result or an error is returned
	// uint64 is used to accomodate the largest count possible
	Incr(context context.Context, key string, count uint, expireIn time.Duration) (uint64, error)
}

// NewIPRateLimiter creates a new IP rate limiter
func NewIPRateLimiter(conf LimitProvider, counter Counter, logger logrus.FieldLogger, reporter MetricReporter) *IPRateLimiter {
	return &IPRateLimiter{conf: conf, counter: counter, logger: logger, reporter: reporter}
}

// IPRateLimiter is an IP based rate limiter
type IPRateLimiter struct {
	conf     LimitProvider
	counter  Counter
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

	key := rl.SlotKey(request, time.Now(), limit.Duration)
	rl.logger.Debugf("generated key %v for request %v", key, request)

	currCount, err := rl.counter.Incr(context, key, 1, limit.Duration)
	if err != nil {
		err = errors.Wrap(err, fmt.Sprintf("error incrementing limit for request %v", request))
		rl.logger.WithError(err).Error("counter returned error when call incr")
		return false, 0, err
	}

	ratelimited = currCount > limit.Count
	if ratelimited {
		rl.logger.Debugf("request %v blocked", request)
		return ratelimited, 0, err // block request, rate limited
	}

	remaining64 := limit.Count - currCount
	remaining32 := uint32(remaining64)
	if uint64(remaining32) != remaining64 { // if we lose some signifcant bits, convert it to max of uint32
		rl.logger.Errorf("overflow detected, setting to max uint32: remaining64 %v remaining32", remaining64, remaining32)
		remaining32 = ^uint32(0)
	}

	rl.logger.Debugf("request %v allowed with %v remaining requests", request, remaining32)
	return ratelimited, remaining32, err
}

// SlotKey generates the key for a slot determined by the request, slot time, and limit duration
func (rl *IPRateLimiter) SlotKey(request Request, slotTime time.Time, duration time.Duration) string {
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
	key := request.RemoteAddress + ":" + strconv.FormatInt(slot, 10)
	return key
}
