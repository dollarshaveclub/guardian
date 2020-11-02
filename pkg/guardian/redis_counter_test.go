package guardian

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis"
	"github.com/go-redis/redis"
)

func newTestRedisCounter(t *testing.T) (*RedisCounter, *miniredis.Miniredis) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("error creating miniredis")
	}

	redis := redis.NewClient(&redis.Options{Addr: s.Addr()})
	return NewRedisCounter(redis, false, TestingLogger, NullReporter{}), s
}

func TestRedisCounterIncr(t *testing.T) {
	c, s := newTestRedisCounter(t)
	defer s.Close()

	key := "test_key"
	namespacedKey := NamespacedKey(limitStoreNamespace, "test_key")
	incrBy := uint(10)
	expire := 1 * time.Second
	maxBlock := uint64(15)

	existingCount := 5
	_, err := s.Incr(namespacedKey, existingCount)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	c.Incr(context.Background(), key, incrBy, maxBlock, expire)
	c.Incr(context.Background(), key, incrBy, maxBlock, expire)

	time.Sleep(1 * time.Second) // wait for async increment

	_, blocked, err := c.Incr(context.Background(), key, 1, maxBlock, expire)
	expectedBlock := true
	if blocked != expectedBlock {
		t.Fatalf("expected: %v received: %v", expectedBlock, blocked)
	}

	expectedCount := uint(existingCount) + incrBy + incrBy
	gotCountStr, err := s.Get(namespacedKey)
	gotCount, _ := strconv.Atoi(gotCountStr)
	if uint(gotCount) != expectedCount {
		t.Fatalf("expected: %v received: %v", expectedCount, gotCount)
	}

	s.FastForward(1 * time.Second)

	_, err = s.Get(namespacedKey)
	expected := miniredis.ErrKeyNotFound
	if err != expected {
		t.Fatalf("expected: %v received: %v", expected, err)
	}
}

func TestPrune(t *testing.T) {
	c, s := newTestRedisCounter(t)
	defer s.Close()

	key := "test_key"
	incrBy := uint(5)
	expire := 1 * time.Second
	maxBlock := uint64(15)

	c.Incr(context.Background(), key, incrBy, maxBlock, expire)
	c.Incr(context.Background(), key, incrBy, maxBlock, expire)

	// sleep required because newTestRedisCounter returns an async counter
	time.Sleep(100 * time.Millisecond)

	// we can't defer c.cache.RUnlock() because c.pruneCache() has to acquire the Lock
	c.cache.RLock()
	_, ok := c.cache.m[key]
	if !ok {
		c.cache.RUnlock()
		t.Fatalf("key should exist in cache but does not")
	}

	c.cache.RUnlock()
	c.pruneCache(time.Now().UTC().Add(2 * time.Second))
	c.cache.RLock()
	defer c.cache.RUnlock()

	_, ok = c.cache.m[key]
	if ok {
		t.Fatalf("key exists in cache when it should not")
	}
}

func TestRemoveAll(t *testing.T) {
	c, s := newTestRedisCounter(t)
	defer s.Close()

	key := "test_key"
	incrBy := uint(5)
	expire := 60 * time.Second
	maxBlock := uint64(15)

	c.Incr(context.Background(), key, incrBy, maxBlock, expire)
	c.Incr(context.Background(), key, incrBy, maxBlock, expire)

	// sleep required because newTestRedisCounter returns an async counter
	time.Sleep(100 * time.Millisecond)

	// we can't defer c.cache.RUnlock() because c.removeAll() has to acquire the Lock
	c.cache.RLock()
	_, ok := c.cache.m[key]
	if !ok {
		c.cache.RUnlock()
		t.Fatalf("key should exist in cache but does not")
	}

	c.cache.RUnlock()
	c.removeAll()
	c.cache.RLock()
	defer c.cache.RUnlock()

	_, ok = c.cache.m[key]
	if ok {
		t.Fatalf("key exists in cache when it should not")
	}
}

func TestRun(t *testing.T) {
	c, s := newTestRedisCounter(t)
	defer s.Close()

	stop := make(chan struct{})
	defer func() { stop <- struct{}{} }()
	go c.Run(100*time.Millisecond, stop)

	key := "test_key"
	incrBy := uint(5)
	expire := 1 * time.Second
	maxBlock := uint64(15)

	c.Incr(context.Background(), key, incrBy, maxBlock, expire)
	c.Incr(context.Background(), key, incrBy, maxBlock, expire)

	// sleep required because newTestRedisCounter returns an async counter
	time.Sleep(100 * time.Millisecond)

	// we can't defer c.cache.RUnlock() because c.pruneCache() has to acquire the Lock
	c.cache.RLock()
	_, ok := c.cache.m[key]
	if !ok {
		c.cache.RUnlock()
		t.Fatalf("key should exist in cache but does not")
	}

	c.cache.RUnlock()
	time.Sleep(2 * time.Second)
	c.cache.RLock()
	defer c.cache.RUnlock()

	_, ok = c.cache.m[key]
	if ok {
		t.Fatalf("key exists in cache when it should not")
	}
}
