package guardian

import (
	"context"
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
	return NewRedisCounter(redis, TestingLogger), s
}

func TestRedisCounterIncr(t *testing.T) {
	c, s := newTestRedisCounter(t)

	key := "test_key"
	namespacedKey := NamespacedKey(limitStoreNamespace, "test_key")
	incrBy := uint(10)
	expire := 1 * time.Second

	existingCount := 5
	_, err := s.Incr(namespacedKey, existingCount)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	count, err := c.Incr(context.Background(), key, incrBy, expire)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	expectedCount := uint(existingCount) + incrBy
	if count != uint64(expectedCount) {
		t.Fatalf("expected: %v received: %v", expectedCount, count)
	}

	s.FastForward(1 * time.Second)

	_, err = s.Get(namespacedKey)
	expected := miniredis.ErrKeyNotFound
	if err != expected {
		t.Fatalf("expected: %v received: %v", expected, err)
	}
}
