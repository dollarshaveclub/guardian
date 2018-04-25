package guardian

import (
	"context"
	"fmt"
	"net"
	"testing"
)

type FakeWhitelistStore struct {
	whitelist   []net.IPNet
	injectedErr error
}

func (f FakeWhitelistStore) GetWhitelist() ([]net.IPNet, error) {
	if f.injectedErr != nil {
		return []net.IPNet{}, f.injectedErr
	}

	return f.whitelist, nil
}

func TestIsWhitelisted(t *testing.T) {
	store := &FakeWhitelistStore{}
	whitelister := NewIPWhitelister(store, TestingLogger)

	tests := []struct {
		// test case setup
		name             string
		storeWhitelist   []net.IPNet
		storeInjectedErr error
		req              Request

		// expected results
		whitelisted bool
		errored     bool
	}{
		{
			name:             "Whitelisted",
			storeWhitelist:   parseCIDRs([]string{"10.0.0.1/24"}),
			storeInjectedErr: nil,
			req:              Request{RemoteAddress: "10.0.0.28"},
			whitelisted:      true,
			errored:          false,
		},
		{
			name:             "NotWhitelisted",
			storeWhitelist:   parseCIDRs([]string{"10.0.0.1/24", "192.1.0.9/32", "192.1.0.7/32"}),
			storeInjectedErr: nil,
			req:              Request{RemoteAddress: "192.1.0.8"},
			whitelisted:      false,
			errored:          false,
		},
		{
			name:             "ErrorFailClosed",
			storeWhitelist:   parseCIDRs([]string{"10.0.0.1/24"}),
			storeInjectedErr: fmt.Errorf("SomeError"),
			req:              Request{RemoteAddress: "192.1.0.8"},
			whitelisted:      false,
			errored:          true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store.whitelist = test.storeWhitelist
			store.injectedErr = test.storeInjectedErr
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

func parseCIDRs(strs []string) []net.IPNet {
	out := []net.IPNet{}

	for _, str := range strs {
		_, cidr, _ := net.ParseCIDR(str)
		out = append(out, *cidr)
	}

	return out
}
