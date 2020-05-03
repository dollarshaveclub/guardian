package guardian

import (
	"context"
	"github.com/go-redis/redis"
	"github.com/sirupsen/logrus"
	"sync"
	"time"
)

// TODO (mk): The counters should know how to slot the keys. this way, they know how to look at previous buckets.
// Also, make sure we persist in Redis for 2x limit.Duration
// That is all, have a great rest of your day :)

const slidingWindowNamespace = "sliding_window_counter"

func NewSlidingWindowCounter(redis *redis.Client, logger logrus.FieldLogger, reporter MetricReporter) *SlidingWindowCounter {
	return &SlidingWindowCounter{
		redis:    redis,
		logger:   logger,
		reporter: reporter,
		cache: &slidingWindowCache{
			m: make(map[string]window),
		},
	}
}

// Window represents a single time window that is exactly limit.Duration long.
// For this sliding window counter implementation, we need to keep track of the previous window and the current window
type window struct {
	val           uint64
	expireAt      time.Time
	startOfWindow time.Time
}

type slidingWindowCache struct {
	sync.RWMutex
	m map[string]window
}

type SlidingWindowCounter struct {
	redis    *redis.Client
	logger   logrus.FieldLogger
	reporter MetricReporter
	cache    *slidingWindowCache
}

func (swc *SlidingWindowCounter) Run(pruneInterval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(pruneInterval)
	for {
		select {
		case <-ticker.C:
			swc.pruneCache(time.Now().UTC())
		case <-stop:
			ticker.Stop()
			return
		}
	}
}

func (swc *SlidingWindowCounter) pruneCache(olderThan time.Time) {
	start := time.Now().UTC()
	cacheSize := 0
	pruned := 0
	defer func() {
		swc.reporter.RedisCounterPruned(time.Since(start), float64(cacheSize), float64(pruned))
	}()

	swc.cache.Lock()
	defer swc.cache.Unlock()

	cacheSize = len(swc.cache.m)
	for k, v := range swc.cache.m {
		if v.expireAt.Before(olderThan) {
			delete(swc.cache.m, k)
			pruned++
		}
	}
}

func (fwc *FixedWindowCounter) Incr(context context.Context, key string, incrBy uint, limit Limit) (uint64, error) {

}
