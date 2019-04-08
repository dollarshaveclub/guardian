package guardian

func IPRateLimiterKeyFunc(req Request) string {
	return req.RemoteAddress
}
