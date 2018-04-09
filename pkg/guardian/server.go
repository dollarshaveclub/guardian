package guardian

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	ratelimit "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"google.golang.org/grpc"
)

func NewServer(blocker RequestBlockerFunc, reportOnly bool, logger logrus.FieldLogger, reporter MetricReporter) *grpc.Server {
	g := grpc.NewServer()
	s := &server{blocker: blocker, reporter: reporter, logger: logger, reportOnly: reportOnly}
	registerRateLimitServiceServer(g, s)

	return g
}

type server struct {
	logger     logrus.FieldLogger
	reporter   MetricReporter
	blocker    RequestBlockerFunc
	reportOnly bool
}

func (s *server) ShouldRateLimit(ctx context.Context, relreq *ratelimit.RateLimitRequest) (*ratelimit.RateLimitResponse, error) {
	start := time.Now()
	req := RequestFromRateLimitRequest(relreq)

	s.logger.Debugf("received rate limit request %v", relreq)
	s.logger.Debugf("converted to request %v", req)

	block, remaining, err := s.blocker(ctx, req)
	if err != nil {
		s.logger.WithError(err).Error("blocker returned error")
	}

	s.logger.Debugf("block: %v, remaining: %v, err: %v", block, remaining, err)

	resp := &ratelimit.RateLimitResponse{
		OverallCode: ratelimit.RateLimitResponse_OK,
	}

	if block && !s.reportOnly {
		resp.OverallCode = ratelimit.RateLimitResponse_OVER_LIMIT
	}

	if s.reportOnly && block {
		s.logger.Infof("would block on request %v", req)
	}

	for i := 0; i < len(relreq.GetDescriptors()); i++ {
		status := &ratelimit.RateLimitResponse_DescriptorStatus{Code: resp.OverallCode, LimitRemaining: remaining}
		resp.Statuses = append(resp.Statuses, status)
	}

	s.logger.Debugf("sending response %v", resp)
	s.reporter.Duration(req, block, time.Since(start))
	return resp, nil
}
