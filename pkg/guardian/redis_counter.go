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

func NewRedisCounter(redis *redis.Client, logger logrus.FieldLogger, reporter MetricReporter) *RedisCounter {
	return &RedisCounter{redis: redis, logger: logger, cache: &lockingExpiringMap{m: make(map[string]item)}, reporter: reporter}
}

type item struct {
	val      uint64
	blocked  bool
	expireAt time.Time
}

type lockingExpiringMap struct {
	sync.RWMutex
	m map[string]item
}

// RedisLimitCounter is a Counter that uses Redis for persistance
// TODO: fetch the current limit configuration from redis instead of using
// a static one
type RedisCounter struct {
	redis    *redis.Client
	logger   logrus.FieldLogger
	reporter MetricReporter
	cache    *lockingExpiringMap
}

func (rs *RedisCounter) Run(pruneInterval time.Duration, stop <-chan struct{}) {
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

func (rs *RedisCounter) Limit(context context.Context, request Request, limit Limit) (bool, uint32, error) {

	key := rs.SlotKey(request, time.Now(), limit.Duration)
	rs.logger.Debugf("generated key %v for request %v", key, request)

	currCount, blocked, err := rs.Incr(context, key, 1, limit.Count, limit.Duration)
	if err != nil {
		err = errors.Wrap(err, fmt.Sprintf("error incrementing limit for request %v", request))
		rs.logger.WithError(err).Error("counter returned error when call incr")
		return false, 0, err
	}

	ratelimited := blocked || currCount > limit.Count
	if ratelimited {
		rs.logger.Debugf("request %v blocked", request)
		return ratelimited, 0, err // block request, rate limited
	}

	remaining64 := limit.Count - currCount
	remaining32 := uint32(remaining64)
	if uint64(remaining32) != remaining64 { // if we lose some signifcant bits, convert it to max of uint32
		rs.logger.Errorf("overflow detected, setting to max uint32: remaining64 %v remaining32", remaining64, remaining32)
		remaining32 = ^uint32(0)
	}

	rs.logger.Debugf("request %v allowed with %v remaining requests", request, remaining32)
	return ratelimited, remaining32, err
}

func (rs *RedisCounter) Incr(context context.Context, key string, incrBy uint, maxBeforeBlock uint64, expireIn time.Duration) (uint64, bool, error) {
	runIncrFunc := func() {
		count, err := rs.doIncr(context, key, incrBy, expireIn)
		if err != nil {
			rs.logger.WithError(err).Error("error incrementing")
			return
		}

		item := item{val: count, blocked: count > maxBeforeBlock, expireAt: time.Now().Add(expireIn)}
		rs.cache.Lock()
		rs.cache.m[key] = item
		rs.cache.Unlock()
	}

	rs.cache.RLock()
	curr := rs.cache.m[key]
	rs.cache.RUnlock()

	if !curr.blocked {
		go runIncrFunc()
	}

	count := curr.val + uint64(incrBy)
	return count, curr.blocked || count > maxBeforeBlock, nil
}

func (rs *RedisCounter) pruneCache(olderThan time.Time) {
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

func (rs *RedisCounter) doIncr(context context.Context, key string, incrBy uint, expireIn time.Duration) (uint64, error) {
	start := time.Now()
	err := error(nil)
	defer func() {
		rs.reporter.RedisCounterIncr(time.Now().Sub(start), err != nil)
	}()

	key = NamespacedKey(limitStoreNamespace, key)

	rs.logger.Debugf("Sending pipeline for key %v INCRBY %v EXPIRE %v", key, incrBy, expireIn.Seconds())

	pipe := rs.redis.Pipeline()
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

	if expireSet == false {
		err = fmt.Errorf("expire timeout not set, key does not exist")
		rs.logger.WithError(err).Error("error executing pipeline")
		return count, err
	}

	rs.logger.Debugf("Successfully executed pipeline and got response: %v %v", count, expireSet)
	return count, nil
}

// SlotKey generates the key the slot and secs for the given slot time
func (rs *RedisCounter) SlotKey(request Request, slotTime time.Time, duration time.Duration) string {
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
