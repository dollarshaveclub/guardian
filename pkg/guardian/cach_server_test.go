package guardian

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCacheServerDeleteCounters(t *testing.T) {
	c, s := newTestRedisCounter(t)
	defer s.Close()
	cacheServer := NewCacheServer("", c)
	testServer := httptest.NewServer(cacheServer.server.Handler)
	defer testServer.Close()

	key := "test_key"
	incrBy := uint(5)
	expire := 1 * time.Second
	maxBlock := uint64(15)

	c.Incr(context.Background(), key, incrBy, maxBlock, expire)
	c.Incr(context.Background(), key, incrBy, maxBlock, expire)

	// sleep required because newTestRedisCounter returns an async counter
	time.Sleep(100 * time.Millisecond)

	req, err := http.NewRequest("DELETE", testServer.URL+"/v0/counters", nil)
	if err != nil {
		t.Fatalf("could not create request: %v", err)
	}
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error from cache server: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status code from cache server: %v", resp.StatusCode)
	}

	c.cache.RLock()
	defer c.cache.RUnlock()
	_, ok := c.cache.m[key]
	if ok {
		t.Fatalf("key exists in cache when it should not")
	}
}
