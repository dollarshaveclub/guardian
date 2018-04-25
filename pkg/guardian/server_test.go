package guardian

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	envoy_api_v2_ratelimit "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
	ratelimit "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
)

type StaticReportOnlyProvider struct {
	reportOnly bool
}

func (s StaticReportOnlyProvider) GetReportOnly() bool {
	return s.reportOnly
}

func newRateLimitRequest() *ratelimit.RateLimitRequest {
	entry := &envoy_api_v2_ratelimit.RateLimitDescriptor_Entry{Key: "somekey", Value: "somevalue"}
	entries := []*envoy_api_v2_ratelimit.RateLimitDescriptor_Entry{entry}
	descr := &envoy_api_v2_ratelimit.RateLimitDescriptor{Entries: entries}
	descrs := []*envoy_api_v2_ratelimit.RateLimitDescriptor{descr}
	return &ratelimit.RateLimitRequest{Domain: "somedomain", HitsAddend: 1, Descriptors: descrs}
}

func newRateLimitResponse(req *ratelimit.RateLimitRequest, code ratelimit.RateLimitResponse_Code, remaining uint32) *ratelimit.RateLimitResponse {
	resp := &ratelimit.RateLimitResponse{
		OverallCode: code,
	}

	for i := 0; i < len(req.GetDescriptors()); i++ {
		status := &ratelimit.RateLimitResponse_DescriptorStatus{Code: resp.OverallCode, LimitRemaining: remaining}
		resp.Statuses = append(resp.Statuses, status)
	}

	return resp
}

func TestShouldRateLimit(t *testing.T) {
	tests := []struct {
		name        string
		blockerFunc RequestBlockerFunc
		reportOnly  bool
		req         *ratelimit.RateLimitRequest
		expectedRes func(*ratelimit.RateLimitRequest) *ratelimit.RateLimitResponse
		expectedErr error
	}{
		{
			name: "ReturnsOverlimitOnBlock",
			blockerFunc: func(c context.Context, req Request) (bool, uint32, error) {
				return true, 0, nil
			},
			reportOnly: false,
			req:        newRateLimitRequest(),
			expectedRes: func(req *ratelimit.RateLimitRequest) *ratelimit.RateLimitResponse {
				return newRateLimitResponse(req, ratelimit.RateLimitResponse_OVER_LIMIT, 0)
			},
			expectedErr: nil,
		},
		{
			name: "ReturnsOKOnNotBlock",
			blockerFunc: func(c context.Context, req Request) (bool, uint32, error) {
				return false, 20, nil
			},
			reportOnly: false,
			req:        newRateLimitRequest(),
			expectedRes: func(req *ratelimit.RateLimitRequest) *ratelimit.RateLimitResponse {
				return newRateLimitResponse(req, ratelimit.RateLimitResponse_OK, 20)
			},
			expectedErr: nil,
		},
		{
			name: "ReturnsOnBlockerBlockOnBlockerErr",
			blockerFunc: func(c context.Context, req Request) (bool, uint32, error) {
				return true, 0, fmt.Errorf("some error")
			},
			reportOnly: false,
			req:        newRateLimitRequest(),
			expectedRes: func(req *ratelimit.RateLimitRequest) *ratelimit.RateLimitResponse {
				return newRateLimitResponse(req, ratelimit.RateLimitResponse_OVER_LIMIT, 0)
			},
			expectedErr: nil,
		},
		{
			name: "ReturnsOkWhenBlockedInReportOnlyMode",
			blockerFunc: func(c context.Context, req Request) (bool, uint32, error) {
				return true, 0, nil
			},
			reportOnly: true,
			req:        newRateLimitRequest(),
			expectedRes: func(req *ratelimit.RateLimitRequest) *ratelimit.RateLimitResponse {
				return newRateLimitResponse(req, ratelimit.RateLimitResponse_OK, 0)
			},
			expectedErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := NewServer(test.blockerFunc, StaticReportOnlyProvider{test.reportOnly}, TestingLogger, NullReporter{})

			res, err := server.ShouldRateLimit(context.Background(), test.req)

			if err != test.expectedErr {
				t.Fatalf("expected: %v, received: %v", test.expectedErr, err)
			}

			er := test.expectedRes(test.req)
			if diff := cmp.Diff(er, res); diff != "" {
				t.Fatalf("expected: %v, received: %v, diff: %v", er, res, diff)
			}
		})
	}
}
