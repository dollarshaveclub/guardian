package guardian

import (
	"context"
	"math"
)

const RequestsRemainingMax = math.MaxUint32

// RequestBlockerFunc is a function that evaluates a given request and determines if it should be blocked or not and how many requests are remaining.
type RequestBlockerFunc func(context.Context, Request) (bool, uint32, error)

// RequestBlockerResp is a type that informs the caller when to stop processing the request, and if the request should be blocked or not.
type RequestBlockerResp string

const (
	// AllowedStop indicates that the request is allowed and that the chain should stop processing.
	AllowedStop RequestBlockerResp = "ALLOWED_STOP"

	// AllowedContinue indicates that the request should be allowed and that the chain should continue processing the request.
	AllowedContinue RequestBlockerResp = "ALLOWED_CONTINUE"

	// BlockedStop indicates that the request should be blocked and that the chain should stop processing the request.
	BlockedStop RequestBlockerResp = "BLOCKED_STOP"
)

// RateLimiter can determine if a request should continue processing, if a request should be blocked, and how many requests are remaining
type RateLimiter interface {
	Limit(ctx context.Context, req Request) (resp RequestBlockerResp, remaining uint32, err error)
}

// CondRequestBlockerFunc is the same as a RequestBlockerFunc with the added ability to indicate that the evaluation of a chain should stop
type CondRequestBlockerFunc func(context.Context, Request) (resp RequestBlockerResp, remaining uint32, err error)

// DefaultCondChain is the default condition chain used by Guardian. This performs the following checks when
// processing a request: whitelist, blacklist, rate limiters.
func DefaultCondChain(whitelister *IPWhitelister, blacklister *IPBlacklister, jailer Jailer, rateLimiters ...RateLimiter) RequestBlockerFunc {
	condWhitelistFunc := CondStopOnWhitelistFunc(whitelister)
	condBlacklistFunc := CondStopOnBlacklistFunc(blacklister)
	condJailerFunc := CondStopOnBanned(jailer)
	rbfs := []CondRequestBlockerFunc{condWhitelistFunc, condBlacklistFunc, condJailerFunc}
	for _, rl := range rateLimiters {
		rbfs = append(rbfs, CondBlockerFromRateLimiter(rl))
	}
	return CondChain(rbfs...)
}

// CondChain chains a series of CondRequestBlockerFunc running each until one indicates the chain should stop processing, returning that functions results
func CondChain(cf ...CondRequestBlockerFunc) RequestBlockerFunc {
	return func(c context.Context, r Request) (bool, uint32, error) {
		minRemaining := uint32(math.MaxUint32)
		for _, f := range cf {
			resp, remaining, err := f(c, r)
			if err != nil {
				// We should never continue processing or block if there is an error, to ensure this fails open
				return false, remaining, err
			}

			if remaining < minRemaining {
				minRemaining = remaining
			}
			switch resp {
			case AllowedStop:
				return false, minRemaining, nil
			case BlockedStop:
				return true, minRemaining, nil
			}
		}
		return false, minRemaining, nil
	}
}

// CondBlockerFromRateLimiter wraps a RateLimiter Limit call
func CondBlockerFromRateLimiter(rl RateLimiter) CondRequestBlockerFunc {
	return func(c context.Context, r Request) (RequestBlockerResp, uint32, error) {
		return rl.Limit(c, r)
	}
}
