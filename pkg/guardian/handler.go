package guardian

import (
	"context"
)

const RequestsRemainingMax = ^uint32(0)

// RequestBlockerFunc is a function that evaluates a given request and determines if it should be blocked or not and how many requests are remaining.
type RequestBlockerFunc func(context.Context, Request) (bool, uint32, error)

func Chain(rf ...RequestBlockerFunc) RequestBlockerFunc {
	chain := func(c context.Context, r Request) (bool, uint32, error) {
		minRemaining := RequestsRemainingMax
		for _, f := range rf {
			blocked, remaining, err := f(c, r)
			if err != nil {
				// let the filter decide if we should fail open or closed
				// but don't trust the remaining returned TODO: reassess this
				return blocked, 0, err
			}

			if remaining < minRemaining {
				minRemaining = remaining
			}

			if blocked {
				return blocked, minRemaining, nil
			}
		}

		return false, minRemaining, nil
	}

	return chain
}

// CondRequestBlockerFunc is the same as a RequestBlockerFunc with the added ability to indicate that the evaluation of a chain should stop
type CondRequestBlockerFunc func(context.Context, Request) (stop, blocked bool, remaining uint32, err error)

// CondChain chains a series of CondRequestBlockerFunc running each until one indicates the chain should stop processing, returning that functions results
func CondChain(cf ...CondRequestBlockerFunc) RequestBlockerFunc {
	chain := func(c context.Context, r Request) (bool, uint32, error) {
		minRemaining := ^uint32(0)
		for _, f := range cf {
			stop, blocked, remaining, err := f(c, r)
			if err != nil && stop {
				return blocked, 0, err
			}

			if remaining < minRemaining {
				minRemaining = remaining
			}

			if stop {
				return blocked, minRemaining, nil
			}
		}

		return false, minRemaining, nil
	}

	return chain
}

// CondStopOnBlockOrError wraps a request blocker function and returns true for stop if the request was blocked or errored out
func CondStopOnBlockOrError(f RequestBlockerFunc) CondRequestBlockerFunc {
	return func(c context.Context, r Request) (bool, bool, uint32, error) {
		blocked, remaining, err := f(c, r)
		stop := (blocked == true || err != nil)

		return stop, blocked, remaining, err
	}
}
