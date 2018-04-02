package guardian

import (
	"context"

	ratelimit "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"google.golang.org/grpc"
)

func NewServer(ratelimiter RateLimitFunc) *grpc.Server {
	g := grpc.NewServer()
	s := &server{}
	ratelimit.RegisterRateLimitServiceServer(g, s)

	return g
}

type server struct {
	ratelimiter RateLimitFunc
}

func (s *server) ShouldRateLimit(ctx context.Context, relreq *ratelimit.RateLimitRequest) (*ratelimit.RateLimitResponse, error) {
	req := RequestFromRateLimitRequest(relreq)
	block, remaining, _ := s.ratelimiter(ctx, req) // eat errors for now until we figure out what envoy does if an error is returned

	resp := &ratelimit.RateLimitResponse{
		OverallCode: ratelimit.RateLimitResponse_OK,
		Statuses:    make([]*ratelimit.RateLimitResponse_DescriptorStatus, len(relreq.GetDescriptors())),
	}
	if block {
		resp.OverallCode = ratelimit.RateLimitResponse_OVER_LIMIT
	}

	for _, status := range resp.GetStatuses() {
		status.Code = resp.OverallCode
		status.LimitRemaining = remaining
	}

	return resp, nil
}
