package guardian

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"path"
	"testing"
	"time"
)

type FakeGlobalLimitProvider struct {
	limit Limit
}

func (lp *FakeGlobalLimitProvider) GetLimit(req Request) Limit {
	return lp.limit
}

type FakeRouteRateLimitProvider struct {
	limits map[url.URL]Limit
}

func (rlp *FakeRouteRateLimitProvider) GetLimit(req Request) Limit {
	reqUrl, err := url.Parse(req.Path)
	if err != nil || reqUrl == nil {
		return Limit{Enabled: false}
	}
	return rlp.limits[*reqUrl]
}

const fakeLimitStoreNamespace = "fake_limit_store"

type FakeLimitStore struct {
	count       map[string]uint64
	injectedErr error
	forceBlock  bool
}

func (fc *FakeLimitStore) Incr(context context.Context, key string, incrBy uint, limit Limit) (uint64, error) {
	if fc.injectedErr != nil {
		return 0, fc.injectedErr
	}

	if fc.forceBlock {
		return limit.Count + 1, nil
	}

	fc.count[key] += uint64(incrBy)
	return fc.count[key], nil
}

func (fc *FakeLimitStore) namespacedKey(key string) string {
	return path.Join(fakeLimitStoreNamespace, key)
}

func TestLimitString(t *testing.T) {
	limit := Limit{Count: 3, Duration: time.Second, Enabled: true}
	got := limit.String()
	expected := "Limit(3 per 1s, enabled: true)"

	if got != expected {
		t.Errorf("expected: %v received: %v", expected, got)
	}
}

func TestLimitRateLimits(t *testing.T) {

	// 3 rps
	limit := Limit{Count: 3, Duration: 1 * time.Second, Enabled: true}
	flp := &FakeGlobalLimitProvider{limit}
	fstore := &FakeLimitStore{count: make(map[string]uint64)}
	rl := &GenericRateLimiter{KeyFunc: IPRateLimiterKeyFunc, LimitProvider: flp, Counter: fstore, Logger: TestingLogger}
	req := Request{RemoteAddress: "192.168.1.2"}
	sentCount := 10

	for i := 0; i < sentCount; i++ {
		blocked, remaining, err := rl.Limit(context.Background(), req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		expectedBlocked := (limit.Count < uint64(i+1))
		if blocked != expectedBlocked {
			t.Fatalf("expected blocked: %v, received blocked: %v", expectedBlocked, blocked)
		}

		expectedRemaining := limit.Count - uint64(i+1)
		if limit.Count < uint64(i+1) {
			expectedRemaining = 0
		}
		if remaining != uint32(expectedRemaining) {
			t.Fatalf("remaining was %d when it should have been %d", remaining, expectedRemaining)
		}
	}
}

func TestDisableLimitDoesNotRateLimit(t *testing.T) {

	limit := Limit{Count: 1, Duration: 1 * time.Second, Enabled: false}
	flp := &FakeGlobalLimitProvider{limit}
	fstore := &FakeLimitStore{count: make(map[string]uint64)}
	rl := &GenericRateLimiter{KeyFunc: IPRateLimiterKeyFunc, LimitProvider: flp, Counter: fstore, Logger: TestingLogger}

	req := Request{RemoteAddress: "192.168.1.2"}
	sentCount := 10

	for i := 0; i < sentCount; i++ {
		blocked, remaining, err := rl.Limit(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedBlocked := false
		if blocked != expectedBlocked {
			t.Fatalf("expected blocked: %v, received blocked: %v", expectedBlocked, blocked)
		}

		expectedRemaining := RequestsRemainingMax
		if remaining != uint32(expectedRemaining) {
			t.Fatalf("remaining was %d when it should have been %d", remaining, expectedRemaining)
		}
	}
}

func TestLimitRateLimitsButThenAllowsAgain(t *testing.T) {

	// 3 rps
	limit := Limit{Count: 3, Duration: 1 * time.Second, Enabled: true}
	flp := &FakeGlobalLimitProvider{limit}
	fstore := &FakeLimitStore{count: make(map[string]uint64)}
	rl := &GenericRateLimiter{KeyFunc: IPRateLimiterKeyFunc, LimitProvider: flp, Counter: fstore, Logger: TestingLogger}

	req := Request{RemoteAddress: "192.168.1.2"}
	sentCount := 10

	for i := 0; i < sentCount; i++ {
		blocked, remaining, err := rl.Limit(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedBlocked := (limit.Count < uint64(i+1))
		if blocked != expectedBlocked {
			t.Fatalf("expected blocked: %v, received blocked: %v", expectedBlocked, blocked)
		}

		expectedRemaining := limit.Count - uint64(i+1)
		if limit.Count < uint64(i+1) {
			expectedRemaining = 0
		}
		if remaining != uint32(expectedRemaining) {
			t.Fatalf("remaining was %d when it should have been %d", remaining, expectedRemaining)
		}
	}

	time.Sleep(limit.Duration)
	blocked, remaining, err := rl.Limit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if blocked != false {
		t.Fatalf("expected blocked: %v, received blocked: %v", false, blocked)
	}
	if remaining != uint32(limit.Count-1) {
		t.Fatalf("remaining was %d when it should have been %d", remaining, uint32(limit.Count-1))
	}
}

func TestLimitRemainingOfflowUsesMaxUInt32(t *testing.T) {

	// 3 rps
	limit := Limit{Count: ^uint64(0), Duration: 1 * time.Second, Enabled: true}
	flp := &FakeGlobalLimitProvider{limit}
	fstore := &FakeLimitStore{count: make(map[string]uint64)}
	rl := &GenericRateLimiter{KeyFunc: IPRateLimiterKeyFunc, LimitProvider: flp, Counter: fstore, Logger: TestingLogger}

	req := Request{RemoteAddress: "192.168.1.2"}
	window := fstore.namespacedKey(IPRateLimiterKeyFunc(req))
	fstore.count[window] = uint64(math.MaxUint32 + 1) // set slot count to some value > max uint32

	blocked, remaining, err := rl.Limit(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if blocked != false {
		t.Fatalf("expected blocked: %v, received blocked: %v", false, blocked)
	}
	if remaining != ^uint32(0) {
		t.Fatalf("remaining was %d when it should have been %d", remaining, ^uint32(0))
	}
}

func TestLimitFailsOpen(t *testing.T) {

	// 3 rps
	limit := Limit{Count: 3, Duration: 1 * time.Second, Enabled: true}
	flp := &FakeGlobalLimitProvider{limit}
	fstore := &FakeLimitStore{count: make(map[string]uint64), injectedErr: fmt.Errorf("some error")}
	rl := &GenericRateLimiter{KeyFunc: IPRateLimiterKeyFunc, LimitProvider: flp, Counter: fstore, Logger: TestingLogger}

	req := Request{RemoteAddress: "192.168.1.2"}

	blocked, _, err := rl.Limit(context.Background(), req)
	if err == nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if blocked != false {
		t.Error("failed closed when it should have failed open")
	}
}

func TestLimitRateLimitsOnBlock(t *testing.T) {

	// 3 rps
	limit := Limit{Count: 3, Duration: 1 * time.Second, Enabled: true}
	flp := &FakeGlobalLimitProvider{limit}
	fstore := &FakeLimitStore{count: make(map[string]uint64), forceBlock: true}
	rl := &GenericRateLimiter{KeyFunc: IPRateLimiterKeyFunc, LimitProvider: flp, Counter: fstore, Logger: TestingLogger}

	req := Request{RemoteAddress: "192.168.1.2"}

	blocked, _, err := rl.Limit(context.Background(), req)
	if err != nil {
		t.Fatalf("expected error but received nothing")
	}

	expected := true
	if blocked != expected {
		t.Fatalf("expected: %v received: %v", expected, blocked)
	}
}

func TestRouteRateLimiter(t *testing.T) {
	fooBarRouteLimit := Limit{Count: 2, Duration: time.Minute, Enabled: true}
	route := url.URL{Path: "/foo/bar"}
	routeLimits := map[url.URL]Limit{route: fooBarRouteLimit}

	flp := &FakeRouteRateLimitProvider{routeLimits}
	fstore := &FakeLimitStore{count: make(map[string]uint64)}
	rl := &GenericRateLimiter{KeyFunc: RouteRateLimiterKeyFunc, LimitProvider: flp, Counter: fstore, Logger: TestingLogger}

	fooBarReq := Request{RemoteAddress: "192.168.1.2", Path: "/foo/bar"}
	fooReq := Request{RemoteAddress: "192.168.1.2", Path: "/foo"}

	sentCount := 10
	for i := 0; i < sentCount; i++ {
		// Ensure routes without limits do not get blocked by this rate limiter
		blocked, remaining, err := rl.Limit(context.Background(), fooReq)
		if err != nil || blocked == true {
			t.Fatalf("unexpected error or blocked request, expected no blocking for request %v", fooReq)
		}

		// Ensure routes with limits do get blocked at the appropriate time
		blocked, remaining, err = rl.Limit(context.Background(), fooBarReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedBlocked := (fooBarRouteLimit.Count < uint64(i+1))
		if blocked != expectedBlocked {
			t.Fatalf("expected blocked: %v, received blocked: %v", expectedBlocked, blocked)
		}

		expectedRemaining := fooBarRouteLimit.Count - uint64(i+1)
		if fooBarRouteLimit.Count < uint64(i+1) {
			expectedRemaining = 0
		}
		if remaining != uint32(expectedRemaining) {
			t.Fatalf("remaining was %d when it should have been %d", remaining, expectedRemaining)
		}
	}
}
