package guardian

import (
	"reflect"
	"testing"

	ratelimit "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
)

func TestRequestConversion(t *testing.T) {
	tests := []struct {
		name  string
		rlreq *ratelimit.RateLimitRequest
		want  Request
	}{{}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := RequestFromRateLimitRequest(test.rlreq)
			if !reflect.DeepEqual(test.want, got) {
				t.Fatalf("wanted %v but got %v", test.want, got)
			}
		})
	}
}
