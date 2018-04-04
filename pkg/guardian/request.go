package guardian

import (
	"strings"

	ratelimit "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
)

const (
	remoteAddressDescriptor = "remote_address"
	authorityDescriptor     = "authority"
	methodDescriptor        = "method"
	pathDescriptor          = "path"
)

const headerDescriptorPrefix = "header."

// Request is an http request
type Request struct {
	RemoteAddress string
	Authority     string
	Method        string
	Path          string
	Headers       map[string]string
}

// RequestFromRateLimitRequest returns a Request from a RateLimitRequest
func RequestFromRateLimitRequest(rlreq *ratelimit.RateLimitRequest) Request {
	req := Request{Headers: make(map[string]string)}
	for _, descriptor := range rlreq.GetDescriptors() {
		for _, e := range descriptor.GetEntries() {
			switch e.GetKey() {
			case remoteAddressDescriptor:
				req.RemoteAddress = e.GetValue()
			case authorityDescriptor:
				req.Authority = e.GetValue()
			case methodDescriptor:
				req.Method = e.GetValue()
			case pathDescriptor:
				req.Path = e.GetValue()
			default:
				if strings.HasPrefix(e.GetKey(), headerDescriptorPrefix) {
					header := strings.TrimPrefix(e.GetKey(), headerDescriptorPrefix)
					req.Headers[header] = e.GetValue()
				}
			}
		}
	}

	return req
}
