package guardian

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const slidingWindowNamespace = "sliding_window_counter"

func NewSlidingWindowCounter(redis *redis.Client, synchronous bool, logger logrus.FieldLogger, reporter MetricReporter) *SlidingWindowCounter {
	return &SlidingWindowCounter{
		redis:       redis,
		synchronous: synchronous,
		logger:      logger,
		reporter:    reporter,
		cache: &slidingWindowCache{
			m: make(map[string]window),
		},
	}
}

// Window represents a single time window that is exactly limit.Duration long.
// For this sliding window counter implementation, we need to keep track of the previous window and the current window
type window struct {
	val      uint64
	expireAt time.Time
	start    time.Time
}

type slidingWindowCache struct {
	sync.RWMutex
	m map[string]window
}

// SlidingWindowCounter maintains a current window and the previous window within its cache in order to simulate a sliding window algorithm.
// It assumes a constant rate of requests during the previous window in order to simplify the approach
// https://blog.cloudflare.com/counting-things-a-lot-of-different-things/
type SlidingWindowCounter struct {
	redis       *redis.Client
	synchronous bool
	logger      logrus.FieldLogger
	reporter    MetricReporter
	cache       *slidingWindowCache
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

func (swc *SlidingWindowCounter) Incr(context context.Context, keyBase string, incrBy uint, limit Limit) (uint64, error) {
	now := time.Now().UTC()
	key := swc.windowKey(keyBase, now, limit.Duration)
	prev := swc.windowKey(keyBase, now.Add(limit.Duration*-1), limit.Duration)
	runIncrFunc := func() (window, error) {
		count, err := swc.doIncr(context, key, incrBy, limit.Duration)
		if err != nil {
			swc.logger.WithError(err).Error("error incrementing")
			return window{}, err
		}

		w := window{
			val:   count,
			start: now,
			// In order to query the counts from the previous window, we need to persist the window for 2x the duration
			expireAt: now.Add(limit.Duration * 2),
		}
		swc.cache.Lock()
		swc.cache.m[key] = w
		swc.cache.Unlock()

		return w, nil
	}

	swc.cache.RLock()
	current := swc.cache.m[key]
	previous := swc.cache.m[prev]
	swc.cache.RUnlock()

	if !swc.synchronous {
		go runIncrFunc()

		offset := (limit.Duration.Seconds() - now.Sub(current.start).Seconds()) / limit.Duration.Seconds()
		ret := previous.val*uint64(offset) + current.val
		count := ret + uint64(incrBy)
		return count, nil
	}

	curr, err := runIncrFunc()
	offset := (limit.Duration.Seconds() - now.Sub(current.start).Seconds()) / limit.Duration.Seconds()
	ret := previous.val*uint64(offset) + curr.val
	return ret, err
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

func (swc *SlidingWindowCounter) doIncr(context context.Context, key string, incrBy uint, expireIn time.Duration) (uint64, error) {
	start := time.Now().UTC()
	var err error
	defer func() {
		swc.reporter.RedisCounterIncr(time.Since(start), err != nil)
	}()

	swc.logger.Debugf("Sending pipeline for key %v INCRBY %v EXPIRE %v", key, incrBy, expireIn.Seconds())
	pipe := swc.redis.Pipeline()
	defer pipe.Close()
	incr := pipe.IncrBy(key, int64(incrBy))
	expire := pipe.Expire(key, expireIn)
	_, err = pipe.Exec()
	if err != nil {
		msg := fmt.Sprintf("error incrementing key %v with increase %d and expiration %v", key, incrBy, expireIn)
		err = errors.Wrap(err, msg)
		swc.logger.WithError(err).Error("error executing pipeline")
		return 0, err
	}

	count := uint64(incr.Val())
	expireSet := expire.Val()

	if !expireSet {
		err = fmt.Errorf("expire timeout not set, key does not exist")
		swc.logger.WithError(err).Error("error executing pipeline")
		return count, err
	}

	swc.logger.Debugf("Successfully executed pipeline and got response: %v %v", count, expireSet)
	return count, nil
}

func (swc *SlidingWindowCounter) windowKey(requestMetadata string, slotTime time.Time, duration time.Duration) string {
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
	return path.Join(fixedWindowNamespace, requestMetadata, strconv.FormatInt(slot, 10))
}
