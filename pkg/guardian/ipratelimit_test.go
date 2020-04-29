package guardian

import (
	"reflect"
	"testing"
	"time"
)

func TestGlobalLimitProvider(t *testing.T) {
	expectedLimit := Limit{Count: 2, Duration: time.Minute, Enabled: true}

	cs, s := newTestConfStoreWithDefaults(t, nil, nil, expectedLimit, false)
	defer s.Close()

	tests := []struct {
		name string
		req  Request
		want Limit
	}{
		{
			name: "empty request",
			req:  Request{},
			want: expectedLimit,
		}, {
			name: "populated request",
			req: Request{
				RemoteAddress: "10.0.0.1",
				Authority:     "foo.bar.com",
				Method:        "GET",
				Path:          "/",
				Headers:       nil,
			},
			want: expectedLimit,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			glp := NewGlobalLimitProvider(cs)
			if got := glp.GetLimit(tt.req); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}
