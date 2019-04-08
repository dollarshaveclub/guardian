package guardian

func RouteRateLimiterKeyFunc(req Request) string {
	return req.RemoteAddress + ":" + req.Path
}
