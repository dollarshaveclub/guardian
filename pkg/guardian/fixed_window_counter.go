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

const fixedWindowNamespace = "fixed_window"

func NewFixedWindowCounter(redis *redis.Client, synchronous bool, logger logrus.FieldLogger, reporter MetricReporter) *FixedWindowCounter {
	return &FixedWindowCounter{redis: redis, synchronous: synchronous, logger: logger, cache: &lockingExpiringMap{m: make(map[string]item)}, reporter: reporter}
}

type item struct {
	val      uint64
	expireAt time.Time
}

type lockingExpiringMap struct {
	sync.RWMutex
	m map[string]item
}

// FixedWindowCounter is a Counter that uses Redis for persistence and atomic increments
type FixedWindowCounter struct {
	redis       *redis.Client
	synchronous bool
	logger      logrus.FieldLogger
	reporter    MetricReporter
	cache       *lockingExpiringMap
}

func (fwc *FixedWindowCounter) Run(pruneInterval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(pruneInterval)
	for {
		select {
		case <-ticker.C:
			fwc.pruneCache(time.Now().UTC())
		case <-stop:
			ticker.Stop()
			return
		}
	}
}

func (fwc *FixedWindowCounter) Incr(context context.Context, key string, incrBy uint, limit Limit) (uint64, error) {
	runIncrFunc := func() (item, error) {
		count, err := fwc.doIncr(context, key, incrBy, limit.Duration)
		if err != nil {
			fwc.logger.WithError(err).Error("error incrementing")
			return item{}, err
		}

		item := item{val: count, expireAt: time.Now().UTC().Add(limit.Duration)}
		fwc.cache.Lock()
		fwc.cache.m[key] = item
		fwc.cache.Unlock()

		return item, nil
	}

	fwc.cache.RLock()
	existing := fwc.cache.m[key]
	fwc.cache.RUnlock()

	// Since this is a fixed window counter, we can assume that the counter only increases during the given window.
	// Therefore, if the existing value is greater than the limit, it's safe to perform this optimization which removes
	// the requirement for communicating with redis at all.
	// However, I think it's a bit confusing to users of the FixedWindowCounter. I am leaving it in here to maintain the
	// previous behavior and because it provides an observable performance boost.
	if existing.val > limit.Count {
		return existing.val + uint64(incrBy), nil
	}

	if !fwc.synchronous {
		go runIncrFunc()

		count := existing.val + uint64(incrBy)
		return count, nil
	}

	curr, err := runIncrFunc()
	return curr.val, err
}

func (fwc *FixedWindowCounter) pruneCache(olderThan time.Time) {
	start := time.Now().UTC()
	cacheSize := 0
	pruned := 0
	defer func() {
		fwc.reporter.RedisCounterPruned(time.Since(start), float64(cacheSize), float64(pruned))
	}()

	fwc.cache.Lock()
	defer fwc.cache.Unlock()

	cacheSize = len(fwc.cache.m)
	for k, v := range fwc.cache.m {
		if v.expireAt.Before(olderThan) {
			delete(fwc.cache.m, k)
			pruned++
		}
	}
}

func (fwc *FixedWindowCounter) doIncr(context context.Context, key string, incrBy uint, expireIn time.Duration) (uint64, error) {
	start := time.Now().UTC()
	var err error
	defer func() {
		fwc.reporter.RedisCounterIncr(time.Since(start), err != nil)
	}()

	fwc.logger.Debugf("Sending pipeline for key %v INCRBY %v EXPIRE %v", key, incrBy, expireIn.Seconds())
	pipe := fwc.redis.Pipeline()
	defer pipe.Close()
	incr := pipe.IncrBy(key, int64(incrBy))
	expire := pipe.Expire(key, expireIn)
	_, err = pipe.Exec()
	if err != nil {
		msg := fmt.Sprintf("error incrementing key %v with increase %d and expiration %v", key, incrBy, expireIn)
		err = errors.Wrap(err, msg)
		fwc.logger.WithError(err).Error("error executing pipeline")
		return 0, err
	}

	count := uint64(incr.Val())
	expireSet := expire.Val()

	if !expireSet {
		err = fmt.Errorf("expire timeout not set, key does not exist")
		fwc.logger.WithError(err).Error("error executing pipeline")
		return count, err
	}

	fwc.logger.Debugf("Successfully executed pipeline and got response: %v %v", count, expireSet)
	return count, nil
}

func (fwc *FixedWindowCounter) windowKey(requestMetadata string, slotTime time.Time, duration time.Duration) string {
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
