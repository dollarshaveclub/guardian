package guardian

import (
	"context"
	"strconv"
	"time"

	"github.com/go-redis/redis"
	"github.com/sirupsen/logrus"
)

const limitStoreNamespace = "limit_store"

func NewRedisSlidingLimiter(redis *redis.Client, logger logrus.FieldLogger, reporter MetricReporter) *RedisCounter {
	return &RedisCounter{redis: redis, logger: logger, cache: &lockingExpiringMap{m: make(map[string]item)}, reporter: reporter}
}

// RedisLimitCounter is a Counter that uses Redis for persistance
// TODO: fetch the current limit configuration from redis instead of using
// a static one
type RedisSlidingLimiter struct {
	redis    *redis.Client
	logger   logrus.FieldLogger
	reporter MetricReporter
	cache    *lockingExpiringMap
}

func (rs *RedisSlidingLimiter) Run(pruneInterval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(pruneInterval)
	for {
		select {
		case <-ticker.C:
			rs.pruneCache(time.Now())
		case <-stop:
			ticker.Stop()
			return
		}
	}
}

func (rs *RedisSlidingLimiter) Limit(context context.Context, request Request, limit Limit) (bool, uint32, error) {
	return false, 0, nil
}

func (rs *RedisSlidingLimiter) pruneCache(olderThan time.Time) {
	start := time.Now()
	cacheSize := 0
	pruned := 0
	defer func() {
		rs.reporter.RedisCounterPruned(time.Now().Sub(start), float64(cacheSize), float64(pruned))
	}()

	rs.cache.Lock()
	defer rs.cache.Unlock()

	cacheSize = len(rs.cache.m)
	for k, v := range rs.cache.m {
		if v.expireAt.Before(olderThan) {
			delete(rs.cache.m, k)
			pruned++
		}
	}
}

type Window struct {
	CurrentSlot      string
	PreviousSlot     string
	PreviousSlotLeft float64 //relative amount of previous slot in window
}

func (rl *RedisSlidingLimiter) Window(request Request, reqTime time.Time, duration time.Duration) Window {

	durNano := int64(duration / time.Nanosecond)
	currNano := reqTime.UnixNano()
	prevNano := reqTime.UnixNano() - durNano

	currSlot := (currNano / durNano) * durNano
	prevSlot := (prevNano / durNano) * durNano

	previousLeft := float64(durNano-(currNano-currSlot)) / float64(durNano)
	currSlotKey := request.RemoteAddress + ":" + strconv.FormatInt(currSlot, 10)
	prevSlotKey := request.RemoteAddress + ":" + strconv.FormatInt(prevSlot, 10)

	return Window{CurrentSlot: currSlotKey, PreviousSlot: prevSlotKey, PreviousSlotLeft: previousLeft}
}
