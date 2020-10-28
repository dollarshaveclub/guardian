package guardian

import (
	"context"
	"net"
	"testing"
)

type FakeBlacklistStore struct {
	blacklist []net.IPNet
}

func (f FakeBlacklistStore) GetBlacklist() []net.IPNet {
	return f.blacklist
}

func TestIsBlacklisted(t *testing.T) {
	store := &FakeBlacklistStore{}
	blacklister := NewIPBlacklister(store, TestingLogger, NullReporter{})

	tests := []struct {
		// test case setup
		name           string
		storeBlacklist []net.IPNet
		req            Request

		// expected results
		blacklisted bool
		errored     bool
	}{
		{
			name:           "Blacklisted",
			storeBlacklist: parseCIDRs([]string{"10.0.0.1/24"}),
			req:            Request{RemoteAddress: "10.0.0.28"},
			blacklisted:    true,
			errored:        false,
		},
		{
			name:           "NotBlacklisted",
			storeBlacklist: parseCIDRs([]string{"10.0.0.1/24", "192.1.0.9/32", "192.1.0.7/32"}),
			req:            Request{RemoteAddress: "192.1.0.8"},
			blacklisted:    false,
			errored:        false,
		},
		{
			name:           "ErrorFailClosed",
			storeBlacklist: parseCIDRs([]string{"10.0.0.1/24"}),
			req:            Request{RemoteAddress: "invalidIP"},
			blacklisted:    false,
			errored:        true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store.blacklist = test.storeBlacklist
			blacklisted, err := blacklister.IsBlacklisted(context.Background(), test.req)
			if test.errored && err == nil {
				t.Error("expected error but received nil")
			}

			if blacklisted != test.blacklisted {
				t.Errorf("expected request %#v to be blacklisted but was not", test.req)
			}
		})
	}
}

func TestCondStopOnBlacklist(t *testing.T) {
	store := &FakeBlacklistStore{blacklist: parseCIDRs([]string{"10.0.0.1/24"})}
	blacklister := NewIPBlacklister(store, TestingLogger, NullReporter{})

	condFunc := CondStopOnBlacklistFunc(blacklister)

	resp, remaining, err := condFunc(context.Background(), Request{RemoteAddress: "10.0.0.2"})

	expectedErr := error(nil)
	expectedResp := BlockedStop
	expectedRemaining := uint32(RequestsRemainingMax)

	if err != expectedErr {
		t.Fatalf("expected: %v received: %v", expectedErr, err)
	}
	if resp != expectedResp {
		t.Fatalf("expected: %v received: %v", expectedResp, resp)
	}
	if remaining != expectedRemaining {
		t.Fatalf("expected: %v received: %v", expectedRemaining, remaining)
	}
}
