package guardian

import (
	"context"
)

// RequestBlockerFunc is a function that evaluates a given request and determines if it should be blocked or not and how many requests are remaining.
type RequestBlockerFunc func(context.Context, Request) (bool, uint32, error)

func Chain(rf ...RequestBlockerFunc) RequestBlockerFunc {
	chain := func(c context.Context, r Request) (bool, uint32, error) {
		minRemaining := ^uint32(0)
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