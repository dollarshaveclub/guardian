package guardian

import (
	"context"
	"net"
	"testing"
)

type FakeWhitelistStore struct {
	whitelist []net.IPNet
}

func (f FakeWhitelistStore) GetWhitelist() []net.IPNet {
	return f.whitelist
}

func TestIsWhitelisted(t *testing.T) {
	store := &FakeWhitelistStore{}
	whitelister := NewIPWhitelister(store, TestingLogger, NullReporter{})

	tests := []struct {
		// test case setup
		name           string
		storeWhitelist []net.IPNet
		req            Request

		// expected results
		whitelisted bool
		errored     bool
	}{
		{
			name:           "Whitelisted",
			storeWhitelist: parseCIDRs([]string{"10.0.0.1/24"}),
			req:            Request{RemoteAddress: "10.0.0.28"},
			whitelisted:    true,
			errored:        false,
		},
		{
			name:           "NotWhitelisted",
			storeWhitelist: parseCIDRs([]string{"10.0.0.1/24", "192.1.0.9/32", "192.1.0.7/32"}),
			req:            Request{RemoteAddress: "192.1.0.8"},
			whitelisted:    false,
			errored:        false,
		},
		{
			name:           "ErrorFailClosed",
			storeWhitelist: parseCIDRs([]string{"10.0.0.1/24"}),
			req:            Request{RemoteAddress: "invalidIP"},
			whitelisted:    false,
			errored:        true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store.whitelist = test.storeWhitelist
			whitelisted, err := whitelister.IsWhitelisted(context.Background(), test.req)
			if test.errored && err == nil {
				t.Error("expected error but received nil")
			}

			if whitelisted != test.whitelisted {
				t.Errorf("expected request %#v to be whitelisted but was not", test.req)
			}
		})
	}
}

func TestCondStopOnWhitelist(t *testing.T) {
	store := &FakeWhitelistStore{whitelist: parseCIDRs([]string{"10.0.0.1/24"})}
	whitelister := NewIPWhitelister(store, TestingLogger, NullReporter{})

	condFunc := CondStopOnWhitelistFunc(whitelister)

	resp, remaining, err := condFunc(context.Background(), Request{RemoteAddress: "10.0.0.2"})

	expectedErr := error(nil)
	expectedResp := AllowedStop
	expectedRemaining := uint32(RequestsRemainingMax)

	if err != expectedErr {
		t.Fatalf("expected: %v received: %v", expectedErr, err)
	}
	if resp != expectedResp {
		t.Fatalf("expected: %v received: %v", expectedResp.String(), resp.String())
	}
	if remaining != expectedRemaining {
		t.Fatalf("expected: %v received: %v", expectedRemaining, remaining)
	}
}

func parseCIDRs(strs []string) []net.IPNet {
	out := []net.IPNet{}

	for _, str := range strs {
		_, cidr, _ := net.ParseCIDR(str)
		out = append(out, *cidr)
	}

	return out
}
