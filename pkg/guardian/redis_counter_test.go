package guardian

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis"
	"github.com/go-redis/redis"
)

func newTestFixedWindowCounter(t *testing.T) (*FixedWindowCounter, *miniredis.Miniredis) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("error creating miniredis")
	}

	redis := redis.NewClient(&redis.Options{Addr: s.Addr()})
	return NewFixedWindowCounter(redis, false, TestingLogger, NullReporter{}), s
}

func TestFixedWindowCounterIncr(t *testing.T) {
	c, s := newTestFixedWindowCounter(t)
	defer s.Close()

	key := "test_key"
	namespacedKey := NamespacedKey(limitStoreNamespace, "test_key")
	incrBy := uint(10)
	limit := Limit{
		Count:    15,
		Duration: 1 * time.Second,
		Enabled:  true,
	}

	existingCount := 5
	_, err := s.Incr(namespacedKey, existingCount)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	c.Incr(context.Background(), incrBy, key, limit)
	c.Incr(context.Background(), incrBy, key, limit)

	time.Sleep(1 * time.Second) // wait for async increment

	curCount, err := c.Incr(context.Background(), 1, key, limit)
	if curCount <= limit.Count {
		t.Fatalf("expected the current count: %d to be greater than the limit count: %v", curCount, limit.Count)
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

func TestFixedWindowPrune(t *testing.T) {
	c, s := newTestFixedWindowCounter(t)
	defer s.Close()

	key := "test_key"
	incrBy := uint(10)
	limit := Limit{
		Count:    15,
		Duration: 1 * time.Second,
		Enabled:  true,
	}

	c.Incr(context.Background(), incrBy, key, limit)
	c.Incr(context.Background(), incrBy, key, limit)

	time.Sleep(time.Second)

	_, ok := c.cache.m[key]
	if !ok {
		t.Fatalf("key should exist in cache but does not")
	}

	c.pruneCache(time.Now().UTC().Add(2 * time.Second))

	_, ok = c.cache.m[key]
	if ok {
		t.Fatalf("key exists in cache when it should not")
	}

}
