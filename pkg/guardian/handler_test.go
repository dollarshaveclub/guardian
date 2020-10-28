package guardian

import (
	"context"
	"fmt"
	"testing"
)

func TestCondChain(t *testing.T) {
	handler1 := func(context.Context, Request) (RequestBlockerResp, uint32, error) {
		return AllowedContinue, 20, nil
	}

	handler2 := func(context.Context, Request) (RequestBlockerResp, uint32, error) {
		return AllowedContinue, 10, nil
	}

	handler3 := func(context.Context, Request) (RequestBlockerResp, uint32, error) {
		return AllowedStop, 1, nil
	}

	blocked, remaining, err := CondChain(handler1, handler2, handler3)(context.Background(), Request{})
	if err != nil {
		t.Fatalf("got err: %v", err)
	}

	if blocked != false {
		t.Fatalf("expected: %v, received: %v", false, blocked)
	}

	expectedRemaining := uint32(1)
	if remaining != expectedRemaining {
		t.Fatalf("expected: %v, received: %v", expectedRemaining, remaining)
	}
}
func TestCondChainStops(t *testing.T) {
	expectedRemaining := uint32(20)
	handler1 := func(context.Context, Request) (RequestBlockerResp, uint32, error) {
		return AllowedContinue, 45, nil
	}

	// handler2 should be where the processing stops, due to its return value
	handler2 := func(context.Context, Request) (RequestBlockerResp, uint32, error) {
		return AllowedStop, expectedRemaining, nil
	}

	handler3 := func(context.Context, Request) (RequestBlockerResp, uint32, error) {
		return AllowedContinue, 5, nil
	}
	blocked, remaining, _ := CondChain(handler1, handler2, handler3)(context.Background(), Request{})

	if expectedRemaining != remaining {
		t.Fatalf("expected remaining: %v, received: %v", expectedRemaining, remaining)
	}

	if blocked != false {
		t.Fatalf("expected blocked: %v, received: %v", false, blocked)
	}
}

func TestCondChainErrStop(t *testing.T) {
	expectedRemaining := uint32(43)
	expectedErr := fmt.Errorf("some error")
	expectedBlock := false
	handler1 := func(context.Context, Request) (RequestBlockerResp, uint32, error) {
		return AllowedContinue, 20, nil
	}

	handler2 := func(context.Context, Request) (RequestBlockerResp, uint32, error) {
		return BlockedStop, expectedRemaining, expectedErr
	}

	handler3 := func(context.Context, Request) (RequestBlockerResp, uint32, error) {
		return AllowedContinue, 20, nil
	}

	blocked, remaining, err := CondChain(handler1, handler2, handler3)(context.Background(), Request{})

	if blocked != expectedBlock {
		t.Fatalf("expected: %v, received: %v", expectedBlock, blocked)
	}

	if remaining != expectedRemaining {
		t.Fatalf("expected: %v, received: %v", expectedRemaining, remaining)
	}

	if err != expectedErr {
		t.Fatalf("expected: %v, received: %v", expectedErr, err)
	}
}

type FakeRateLimiter struct {
	rbf CondRequestBlockerFunc
}

func (frl *FakeRateLimiter) Limit(c context.Context, req Request) (RequestBlockerResp, uint32, error) {
	return frl.rbf(c, req)
}

func TestCondBlockerFromRateLimiter(t *testing.T) {
	handlerBlocked := &FakeRateLimiter{
		rbf: func(context.Context, Request) (RequestBlockerResp, uint32, error) {
			return BlockedStop, 20, nil
		},
	}

	handlerErr := &FakeRateLimiter{
		rbf: func(context.Context, Request) (RequestBlockerResp, uint32, error) {
			return AllowedStop, 20, fmt.Errorf("some error")
		},
	}
	resp, _, _ := CondBlockerFromRateLimiter(handlerBlocked)(context.Background(), Request{})
	if resp != BlockedStop {
		t.Fatalf("expected %v, received: %v", AllowedStop.String(), resp.String())
	}

	resp, _, _ = CondBlockerFromRateLimiter(handlerErr)(context.Background(), Request{})
	if resp != AllowedStop {
		t.Fatalf("expected %v, received: %v", AllowedStop.String(), resp.String())
	}
}
