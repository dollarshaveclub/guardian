package guardian

import (
	"testing"

	envoy_api_v2_ratelimit "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
	ratelimit "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"github.com/google/go-cmp/cmp"
)

func TestRequestConversion(t *testing.T) {
	tests := []struct {
		name  string
		rlreq *ratelimit.RateLimitRequest
		want  Request
	}{{
		name: "NoHeaders",
		rlreq: rateLimitRequestWithKeyValues([]kv{
			kv{k: remoteAddressDescriptor, v: "10.0.0.123"},
			kv{k: authorityDescriptor, v: "www.shave.io"},
			kv{k: methodDescriptor, v: "GET"},
			kv{k: pathDescriptor, v: "/somePath"},
		}),
		want: Request{
			RemoteAddress: "10.0.0.123",
			Authority:     "www.shave.io",
			Method:        "GET",
			Path:          "/somePath",
			Headers:       make(map[string]string),
		},
	}, {
		name: "WithHeaders",
		rlreq: rateLimitRequestWithKeyValues([]kv{
			kv{k: remoteAddressDescriptor, v: "10.0.0.123"},
			kv{k: authorityDescriptor, v: "www.shave.io"},
			kv{k: methodDescriptor, v: "GET"},
			kv{k: pathDescriptor, v: "/somePath"},
			kv{k: headerDescriptorPrefix + "x-forwarded-for", v: "192.168.1.223, 10.10.0.23"},
		}),
		want: Request{
			RemoteAddress: "10.0.0.123",
			Authority:     "www.shave.io",
			Method:        "GET",
			Path:          "/somePath",
			Headers:       map[string]string{"x-forwarded-for": "192.168.1.223, 10.10.0.23"},
		},
	},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := RequestFromRateLimitRequest(test.rlreq)
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("got want differs: (-got +want)\n%s", diff)
			}
		})
	}
}

type kv struct {
	k string
	v string
}

func rateLimitRequestWithKeyValues(kvs []kv) *ratelimit.RateLimitRequest {
	req := &ratelimit.RateLimitRequest{
		Domain:     "some.domain",
		HitsAddend: 1,
	}

	for _, kv := range kvs {
		entry := &envoy_api_v2_ratelimit.RateLimitDescriptor_Entry{
			Key:   kv.k,
			Value: kv.v,
		}

		descriptor := &envoy_api_v2_ratelimit.RateLimitDescriptor{
			Entries: []*envoy_api_v2_ratelimit.RateLimitDescriptor_Entry{entry},
		}
		req.Descriptors = append(req.Descriptors, descriptor)
	}

	return req
}
