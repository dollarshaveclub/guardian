package guardian

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	ratelimit "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
)

type ReportOnlyProvider interface {
	GetReportOnly() bool
}

func NewServer(blocker RequestBlockerFunc, reportOnlyProvider ReportOnlyProvider, logger logrus.FieldLogger, reporter MetricReporter) *Server {
	return &Server{blocker: blocker, roProvider: reportOnlyProvider, reporter: reporter, logger: logger}
}

type Server struct {
	roProvider ReportOnlyProvider
	logger     logrus.FieldLogger
	reporter   MetricReporter
	blocker    RequestBlockerFunc
}

func (s *Server) ShouldRateLimit(ctx context.Context, relreq *ratelimit.RateLimitRequest) (*ratelimit.RateLimitResponse, error) {
	start := time.Now().UTC()
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

	reportOnly := s.roProvider.GetReportOnly()
	s.reporter.CurrentReportOnlyMode(reportOnly)

	if block && !reportOnly {
		resp.OverallCode = ratelimit.RateLimitResponse_OVER_LIMIT
	}

	if block {
		s.logger.Infof("would block on request %v", req)
	}

	for i := 0; i < len(relreq.GetDescriptors()); i++ {
		status := &ratelimit.RateLimitResponse_DescriptorStatus{Code: resp.OverallCode, LimitRemaining: remaining}
		resp.Statuses = append(resp.Statuses, status)
	}

	s.logger.Debugf("sending response %v", resp)
	s.reporter.Duration(req, block, err != nil, time.Since(start))
	return resp, nil
}
