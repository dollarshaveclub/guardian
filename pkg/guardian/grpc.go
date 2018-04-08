package guardian

import (
	"context"

	ratelimit "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"google.golang.org/grpc"
)

// So this is mostly copy past from https://github.com/envoyproxy/go-control-plane/blob/v0.1/envoy/service/ratelimit/v2/rls.pb.go#L286
// but with the correct ServiceName and FullMethod
// Envoy will eventually switch to the proto linked above: https://github.com/envoyproxy/envoy/issues/1034
func registerRateLimitServiceServer(s *grpc.Server, srv ratelimit.RateLimitServiceServer) {
	s.RegisterService(&_rateLimitService_serviceDesc, srv)
}

func _rateLimitService_ShouldRateLimit_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ratelimit.RateLimitRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ratelimit.RateLimitServiceServer).ShouldRateLimit(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/pb.lyft.ratelimit.RateLimitService/ShouldRateLimit",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ratelimit.RateLimitServiceServer).ShouldRateLimit(ctx, req.(*ratelimit.RateLimitRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _rateLimitService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "pb.lyft.ratelimit.RateLimitService",
	HandlerType: (*ratelimit.RateLimitServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "ShouldRateLimit",
			Handler:    _rateLimitService_ShouldRateLimit_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "envoy/service/ratelimit/v2/rls.proto",
}
