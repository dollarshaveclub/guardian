package guardian

import (
	"context"
	"fmt"
	"testing"
)

func TestChainInOrder(t *testing.T) {
	order := []int{}
	handler1 := func(context.Context, Request) (bool, uint32, error) {
		order = append(order, 0)
		return false, 0, nil
	}

	handler2 := func(context.Context, Request) (bool, uint32, error) {
		order = append(order, 1)
		return false, 0, nil
	}

	handler3 := func(context.Context, Request) (bool, uint32, error) {
		order = append(order, 2)
		return false, 0, nil
	}

	chained := Chain(handler1, handler2, handler3)

	chained(context.Background(), Request{})

	if len(order) != 3 {
		t.Fatalf("Expect %v handler calls but got %v", 3, len(order))
	}

	for i, v := range order {
		if i != v {
			t.Fatal("Handlers occurred out of order")
		}
	}
}

func TestChainStopsWhenRequestBlocked(t *testing.T) {
	stopped := true
	handler1 := func(context.Context, Request) (bool, uint32, error) {
		return true, 0, nil
	}

	handler2 := func(context.Context, Request) (bool, uint32, error) {
		stopped = false
		return false, 0, nil
	}

	chained := Chain(handler1, handler2)

	chained(context.Background(), Request{})

	if stopped == false {
		t.Fatalf("did not stop when handler1 blocked request")
	}
}

func TestChainStopsOnError(t *testing.T) {
	stopped := true
	handler1 := func(context.Context, Request) (bool, uint32, error) {
		return false, 0, fmt.Errorf("some error")
	}

	handler2 := func(context.Context, Request) (bool, uint32, error) {
		stopped = false
		return false, 0, nil
	}

	chained := Chain(handler1, handler2)

	chained(context.Background(), Request{})

	if stopped == false {
		t.Fatalf("did not stop when handler1 blocked request")
	}
}

func TestChainErrorHandler(t *testing.T) {
	stopped := true
	handler1 := func(context.Context, Request) (bool, uint32, error) {
		return true, 0, fmt.Errorf("some error")
	}

	handler2 := func(context.Context, Request) (bool, uint32, error) {
		stopped = false
		return false, 0, nil
	}

	chained := Chain(handler1, handler2)

	blocked, remaining, err := chained(context.Background(), Request{})

	if stopped == false {
		t.Fatalf("did not stop when handler1 blocked request")
	}

	if err == nil {
		t.Fatal("Error was nil when it shouldn't have been")
	}

	if blocked != true {
		t.Fatal("blocked was false when it should have been true")
	}

	if remaining != 0 {
		t.Fatalf("Remaining was %v when it should have been %v", remaining, 0)
	}
}
