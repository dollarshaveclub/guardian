package guardian

import (
	"context"
	"path"
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

	redis := redis.NewClient(&redis.Options{
		Addr:         s.Addr(),
		MaxRetries:   3,
		IdleTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})
	return NewFixedWindowCounter(redis, false, TestingLogger, NullReporter{}), s
}

func TestFixedWindowIncr(t *testing.T) {
	c, s := newTestFixedWindowCounter(t)
	defer s.Close()
	limit := Limit{15, time.Hour, true}
	key := "test_key"
	incrBy := uint(10)
	existingCount := 5
	start := time.Now().UTC()
	_, err := s.Incr(c.windowKey(key, start, limit.Duration), existingCount)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	c.Incr(context.Background(), "test_key", incrBy, limit)
	c.Incr(context.Background(), "test_key", incrBy, limit)

	time.Sleep(1 * time.Second) // wait for async increment

	curCount, err := c.Incr(context.Background(), "test_key", 1, limit)
	if curCount < limit.Count {
		t.Fatalf("expected current count to be greater than limit: %d received: %d", limit.Count, curCount)
	}

	expectedCount := uint(existingCount) + incrBy + incrBy
	gotCountStr, err := s.Get(c.windowKey(key, start, limit.Duration))
	gotCount, _ := strconv.Atoi(gotCountStr)
	if uint(gotCount) != expectedCount {
		t.Fatalf("expected: %v received: %v", expectedCount, gotCount)
	}

	s.FastForward(1 * time.Hour)

	_, err = s.Get(key)
	expected := miniredis.ErrKeyNotFound
	if err != expected {
		t.Fatalf("expected: %v received: %v", expected, err)
	}
}

func TestPrune(t *testing.T) {
	c, s := newTestFixedWindowCounter(t)
	defer s.Close()

	key := "test_key"
	incrBy := uint(5)
	limit := Limit{15, time.Second, true}

	c.Incr(context.Background(), key, incrBy, limit)
	c.Incr(context.Background(), key, incrBy, limit)

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

func TestFixedWindowKey(t *testing.T) {
	c, s := newTestFixedWindowCounter(t)
	defer s.Close()
	referenceRequest := Request{RemoteAddress: "192.168.1.2"}
	referenceTime := time.Unix(1522969710, 0)

	tests := []struct {
		name          string
		request       Request
		requestTime   time.Time
		limitDuration time.Duration
		want          string
	}{
		{
			name:          "BucketSameSecond",
			request:       referenceRequest,
			requestTime:   referenceTime,
			limitDuration: 10 * time.Second,
			want:          path.Join(fixedWindowNamespace, referenceRequest.RemoteAddress, "1522969710"),
		},
		{
			name:          "BucketRoundsDown",
			request:       referenceRequest,
			requestTime:   referenceTime.Add(5 * time.Second),
			limitDuration: 10 * time.Second,
			want:          path.Join(fixedWindowNamespace, referenceRequest.RemoteAddress, "1522969710"),
		},
		{
			name:          "BucketNext",
			request:       referenceRequest,
			requestTime:   referenceTime.Add(10 * time.Second),
			limitDuration: 10 * time.Second,
			want:          path.Join(fixedWindowNamespace, referenceRequest.RemoteAddress, "1522969720"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := c.windowKey(IPRateLimiterKeyFunc(test.request), test.requestTime, test.limitDuration)
			if got != test.want {
				t.Errorf("got %v, wanted %v", got, test.want)
			}
		})
	}

}
