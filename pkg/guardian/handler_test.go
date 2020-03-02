package guardian

import (
	"context"
	"fmt"
	"testing"
)

func TestCondChain(t *testing.T) {
	stopped := true
	handler1 := func(context.Context, Request) (bool, bool, uint32, error) {
		return false, false, 20, nil
	}

	handler2 := func(context.Context, Request) (bool, bool, uint32, error) {
		return false, false, 10, nil
	}

	handler3 := func(context.Context, Request) (bool, bool, uint32, error) {
		stopped = false
		return false, false, 1, nil
	}

	blocked, remaining, err := CondChain(handler1, handler2, handler3)(context.Background(), Request{})
	if err != nil {
		t.Fatalf("got err: %v", err)
	}

	if stopped != false {
		t.Fatalf("expected: %v, received: %v", false, stopped)
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
	stopped := true
	handler1 := func(context.Context, Request) (bool, bool, uint32, error) {
		return false, false, 20, nil
	}

	handler2 := func(context.Context, Request) (bool, bool, uint32, error) {
		return true, false, 20, nil
	}

	handler3 := func(context.Context, Request) (bool, bool, uint32, error) {
		stopped = false
		return true, false, 20, nil
	}

	CondChain(handler1, handler2, handler3)(context.Background(), Request{})

	if stopped != true {
		t.Fatalf("expected: %v, received: %v", true, stopped)
	}
}

func TestCondChainErrStop(t *testing.T) {
	stopped := true
	expectedErr := fmt.Errorf("some error")
	expectedBlock := true
	handler1 := func(context.Context, Request) (bool, bool, uint32, error) {
		return false, false, 20, nil
	}

	handler2 := func(context.Context, Request) (bool, bool, uint32, error) {
		return true, expectedBlock, 20, expectedErr
	}

	handler3 := func(context.Context, Request) (bool, bool, uint32, error) {
		stopped = false
		return true, false, 20, nil
	}

	blocked, remaining, err := CondChain(handler1, handler2, handler3)(context.Background(), Request{})

	if stopped != true {
		t.Fatalf("expected: %v, received: %v", true, stopped)
	}

	if blocked != expectedBlock {
		t.Fatalf("expected: %v, received: %v", expectedBlock, blocked)
	}

	expectedRemaining := uint32(0)
	if remaining != expectedRemaining {
		t.Fatalf("expected: %v, received: %v", expectedRemaining, remaining)
	}

	if err != expectedErr {
		t.Fatalf("expected: %v, received: %v", expectedErr, err)
	}
}

func TestCondStopOnBlockOrError(t *testing.T) {
	handlerBlocked := func(context.Context, Request) (bool, uint32, error) {
		return true, 20, nil
	}

	handlerErr := func(context.Context, Request) (bool, uint32, error) {
		return false, 20, fmt.Errorf("some error")
	}

	expected := true
	stop, _, _, _ := CondStopOnBlockOrError(handlerBlocked)(context.Background(), Request{})

	if stop != expected {
		t.Fatalf("expected: %v, received: %v", expected, stop)
	}

	stop, _, _, _ = CondStopOnBlockOrError(handlerErr)(context.Background(), Request{})
	if stop != expected {
		t.Fatalf("expected: %v, received: %v", expected, stop)
	}
}
