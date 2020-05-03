package guardian

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const limitStoreNamespace = "limit_store"

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

// RedisLimitCounter is a Counter that uses Redis for persistence
type FixedWindowCounter struct {
	redis       *redis.Client
	synchronous bool
	logger      logrus.FieldLogger
	reporter    MetricReporter
	cache       *lockingExpiringMap
}

func (rs *FixedWindowCounter) Run(pruneInterval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(pruneInterval)
	for {
		select {
		case <-ticker.C:
			rs.pruneCache(time.Now().UTC())
		case <-stop:
			ticker.Stop()
			return
		}
	}
}

func (rs *FixedWindowCounter) Incr(context context.Context, incrBy uint, keybase string, limit Limit) (uint64, error) {
	key := slotKey(keybase, time.Now().UTC(), limit.Duration)
	runIncrFunc := func() (item, error) {
		count, err := rs.doIncr(context, key, incrBy, limit.Duration)
		if err != nil {
			rs.logger.WithError(err).Error("error incrementing")
			return item{}, err
		}

		item := item{val: count, expireAt: time.Now().UTC().Add(limit.Duration)}
		rs.cache.Lock()
		rs.cache.m[key] = item
		rs.cache.Unlock()
		return item, nil
	}

	rs.cache.RLock()
	existing := rs.cache.m[key]
	rs.cache.RUnlock()

	if !rs.synchronous {
		go runIncrFunc()

		count := existing.val + uint64(incrBy)
		return count, nil
	}

	curr, err := runIncrFunc()
	return curr.val, err
}

func (rs *FixedWindowCounter) pruneCache(olderThan time.Time) {
	start := time.Now().UTC()
	cacheSize := 0
	pruned := 0
	defer func() {
		rs.reporter.RedisCounterPruned(time.Since(start), float64(cacheSize), float64(pruned))
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

func (rs *FixedWindowCounter) doIncr(context context.Context, key string, incrBy uint, expireIn time.Duration) (uint64, error) {
	start := time.Now().UTC()
	var err error
	defer func() {
		rs.reporter.RedisCounterIncr(time.Since(start), err != nil)
	}()

	key = NamespacedKey(limitStoreNamespace, key)

	rs.logger.Debugf("Sending pipeline for key %v INCRBY %v EXPIRE %v", key, incrBy, expireIn.Seconds())

	pipe := rs.redis.Pipeline()
	defer pipe.Close()
	incr := pipe.IncrBy(key, int64(incrBy))
	expire := pipe.Expire(key, expireIn)
	_, err = pipe.Exec()
	if err != nil {
		msg := fmt.Sprintf("error incrementing key %v with increase %d and expiration %v", key, incrBy, expireIn)
		err = errors.Wrap(err, msg)
		rs.logger.WithError(err).Error("error executing pipeline")
		return 0, err
	}

	count := uint64(incr.Val())
	expireSet := expire.Val()

	if !expireSet {
		err = fmt.Errorf("expire timeout not set, key does not exist")
		rs.logger.WithError(err).Error("error executing pipeline")
		return count, err
	}

	rs.logger.Debugf("Successfully executed pipeline and got response: %v %v", count, expireSet)
	return count, nil
}

// slotKey generates the key for a slot determined by the request, slot time, and limit duration
func slotKey(keybase string, slotTime time.Time, duration time.Duration) string {
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
