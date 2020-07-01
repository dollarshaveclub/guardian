package guardian

import (
	"reflect"
	"testing"
	"time"
)

func TestRouteLimitProvider(t *testing.T) {
	route := "/foo/bar"
	fooBarRateLimit := RateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: RateLimitConfigVersion,
			Kind:    RateLimitConfigKind,
			Name:    route,
		},
		Spec: RateLimitSpec{
			Limit: Limit{
				Count:    2,
				Duration: time.Minute,
				Enabled:  true,
			},
			Conditions: Conditions{
				Path: route,
			},
		},
	}
	globalLimit := Limit{Count: 2, Duration: time.Minute, Enabled: true}
	cs, s := newTestConfStoreWithDefaults(t, nil, nil, globalLimit, false)
	defer s.Close()

	cs.ApplyRateLimitConfig(fooBarRateLimit)
	cs.UpdateCachedConf()

	tests := []struct {
		name      string
		req       Request
		wantLimit Limit
	}{
		{
			name:      "route with limit",
			req:       Request{Path: "/foo/bar"},
			wantLimit: fooBarRateLimit.Spec.Limit,
		},
		{
			name:      "sub route without limit",
			req:       Request{Path: "/foo/bar/baz"},
			wantLimit: Limit{Enabled: false},
		},
		{
			name:      "route without limit",
			req:       Request{Path: "/baz"},
			wantLimit: Limit{Enabled: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rlp := NewRouteRateLimitProvider(cs, TestingLogger)
			if gotLimit := rlp.GetLimit(tt.req); !reflect.DeepEqual(gotLimit, tt.wantLimit) {
				t.Errorf("GetLimit() = %v, want %v", gotLimit, tt.wantLimit)
			}
		})
	}
}

func TestRouteLimitProviderUpdates(t *testing.T) {
	route := "/foo/bar"
	fooBarRateLimit := RateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: RateLimitConfigVersion,
			Kind:    RateLimitConfigKind,
			Name:    route,
		},
		Spec: RateLimitSpec{
			Limit: Limit{
				Count:    2,
				Duration: time.Minute,
				Enabled:  true,
			},
			Conditions: Conditions{
				Path: route,
			},
		},
	}
	globalLimit := Limit{Count: 2, Duration: time.Minute, Enabled: true}
	cs, s := newTestConfStoreWithDefaults(t, nil, nil, globalLimit, false)
	defer s.Close()

	cs.ApplyRateLimitConfig(fooBarRateLimit)
	cs.UpdateCachedConf()

	rlp := NewRouteRateLimitProvider(cs, TestingLogger)
	gotLimit := rlp.GetLimit(Request{Path: "/foo/bar"})
	if !reflect.DeepEqual(gotLimit, fooBarRateLimit.Spec.Limit) {
		t.Errorf("GetLimit() = %v, want %v", gotLimit, fooBarRateLimit.Spec.Limit)
	}

	fooBarRateLimit = RateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: RateLimitConfigVersion,
			Kind:    RateLimitConfigKind,
			Name:    route,
		},
		Spec: RateLimitSpec{
			Limit: Limit{
				Count:    43,
				Duration: time.Minute,
				Enabled:  true,
			},
			Conditions: Conditions{
				Path: route,
			},
		},
	}
	cs.ApplyRateLimitConfig(fooBarRateLimit)
	cs.UpdateCachedConf()

	gotLimit = rlp.GetLimit(Request{Path: "/foo/bar"})
	if !reflect.DeepEqual(gotLimit, fooBarRateLimit.Spec.Limit) {
		t.Errorf("GetLimit() = %v, want %v", gotLimit, fooBarRateLimit.Spec.Limit)
	}
}
