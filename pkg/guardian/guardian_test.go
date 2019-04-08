package guardian

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis"
	envoy_api_v2_ratelimit "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
	ratelimit "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"github.com/go-redis/redis"
	"github.com/sirupsen/logrus"
)

func newAcceptanceGuardianServer(t *testing.T, logger logrus.FieldLogger) (*Server, *miniredis.Miniredis, *RedisConfStore, chan struct{}) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("error creating miniredis")
	}

	stop := make(chan struct{})
	redis := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	if res := redis.Ping(); res.Err() != nil {
		t.Fatalf("error pinging redis: %v", res.Err())
	}
	redisConfStore := NewRedisConfStore(redis, []net.IPNet{}, []net.IPNet{}, Limit{Count: 15, Duration: time.Second}, false, logger.WithField("context", "redis-conf-provider"))
	redisCounter := NewRedisCounter(redis, false, logger.WithField("context", "redis-counter"), NullReporter{})
	go redisConfStore.RunSync(1*time.Second, stop)

	whitelister := NewIPWhitelister(redisConfStore, logger.WithField("context", "ip-whitelister"), NullReporter{})
	blacklister := NewIPBlacklister(redisConfStore, logger.WithField("context", "ip-blacklister"), NullReporter{})
	rateLimiter := &GenericRateLimiter{
		KeyFunc:  IPRateLimiterKeyFunc,
		Conf:     redisConfStore,
		Counter:  redisCounter,
		Logger:   logger.WithField("context", "ip-rate-limiter"),
		Reporter: NullReporter{},
	}

	condFuncChain := DefaultCondChain(whitelister, blacklister, rateLimiter)
	server := NewServer(condFuncChain, redisConfStore, logger.WithField("context", "server"), NullReporter{})

	return server, mr, redisConfStore, stop
}

func newAcceptanceRateLimtRequest(t *testing.T, remoteAddress, authority, method, path string) *ratelimit.RateLimitRequest {
	t.Helper()
	req := &ratelimit.RateLimitRequest{
		Domain:     "edge_proxy_per_ip",
		HitsAddend: 1,
	}

	kvs := []struct {
		key   string
		value string
	}{
		{key: remoteAddressDescriptor, value: remoteAddress},
		{key: authorityDescriptor, value: authority},
		{key: methodDescriptor, value: method},
		{key: pathDescriptor, value: path},
	}

	for _, kv := range kvs {
		entry := &envoy_api_v2_ratelimit.RateLimitDescriptor_Entry{
			Key:   kv.key,
			Value: kv.value,
		}

		descriptor := &envoy_api_v2_ratelimit.RateLimitDescriptor{
			Entries: []*envoy_api_v2_ratelimit.RateLimitDescriptor_Entry{entry},
		}
		req.Descriptors = append(req.Descriptors, descriptor)
	}

	return req
}

func newAcceptanceRateLimitResponse(t *testing.T, req *ratelimit.RateLimitRequest, code ratelimit.RateLimitResponse_Code, remaining uint32) *ratelimit.RateLimitResponse {
	t.Helper()
	resp := &ratelimit.RateLimitResponse{
		OverallCode: code,
	}

	for i := 0; i < len(req.GetDescriptors()); i++ {
		status := &ratelimit.RateLimitResponse_DescriptorStatus{Code: resp.OverallCode, LimitRemaining: remaining}
		resp.Statuses = append(resp.Statuses, status)
	}

	return resp
}

func ipStringToIPNet(t *testing.T, ip string) net.IPNet {
	t.Helper()
	full := fmt.Sprintf("%v/32", ip)
	_, cidr, err := net.ParseCIDR(full)
	if err != nil {
		t.Fatalf("error parsing cidr: %v", err)
	}

	return *cidr
}

func TestBasicFunctionality(t *testing.T) {

	logger := logrus.StandardLogger()
	server, miniredis, redisConfStore, stop := newAcceptanceGuardianServer(t, logger)
	defer func() {
		miniredis.Close()
	}()
	defer func() {
		close(stop)
		time.Sleep(50 * time.Millisecond) // give the redisConfStore.RunSync() goroutine time to exit
	}()

	whitelistedIP := "10.10.10.10"
	blacklistedIP := "11.11.11.11"
	ratelimitedIP := "12.12.12.12"

	authority := "google.com"
	method := "GET"
	path := "/"

	redisConfStore.AddWhitelistCidrs([]net.IPNet{ipStringToIPNet(t, whitelistedIP)})
	redisConfStore.AddBlacklistCidrs([]net.IPNet{ipStringToIPNet(t, blacklistedIP)})
	redisConfStore.SetLimit(Limit{Count: 5, Duration: time.Minute, Enabled: true})
	redisConfStore.SetReportOnly(false)

	time.Sleep(2 * time.Second) // let conf changes take effect

	ctx := context.Background()

	// whitelisted ip
	for i := 0; i < 10; i++ {
		res, err := server.ShouldRateLimit(ctx, newAcceptanceRateLimtRequest(t, whitelistedIP, authority, method, path))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := ratelimit.RateLimitResponse_OK
		if res.GetOverallCode() != want {
			t.Fatalf("want %v, got %v", want, res.GetOverallCode())
		}
	}

	// blacklisted ip
	for i := 0; i < 10; i++ {
		res, err := server.ShouldRateLimit(ctx, newAcceptanceRateLimtRequest(t, blacklistedIP, authority, method, path))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := ratelimit.RateLimitResponse_OVER_LIMIT
		if res.GetOverallCode() != want {
			t.Fatalf("want %v, got %v", want, res.GetOverallCode())
		}
	}

	// rate limited IP
	// we sleep every iteration due to rate limiting have an asynchronous implementation
	for i := 0; i < 10; i++ {
		time.Sleep(5 * time.Millisecond)
		res, err := server.ShouldRateLimit(ctx, newAcceptanceRateLimtRequest(t, ratelimitedIP, authority, method, path))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := ratelimit.RateLimitResponse_OK
		if i > 4 {
			want = ratelimit.RateLimitResponse_OVER_LIMIT
		}

		if res.OverallCode != want {
			t.Fatalf("want %v, got %v, iteration %v", want, res.OverallCode, i)
		}
	}
}
