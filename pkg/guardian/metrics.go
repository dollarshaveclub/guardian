package guardian

import (
	"fmt"
	"time"

	"github.com/DataDog/datadog-go/statsd"
)

type MetricReporter interface {
	Duration(request Request, blocked bool, duration time.Duration) error
}

type DataDogReporter struct {
	Client *statsd.Client
}

const durationMetricName = "request.duration"
const blockedKey = "blocked"
const authorityKey = "authority"

func (d *DataDogReporter) Duration(request Request, blocked bool, duration time.Duration) error {
	authorityTag := fmt.Sprintf("%v: %v", authorityKey, request.Authority)
	blockedTag := fmt.Sprintf("%v: %v", blockedKey, blocked)
	return d.Client.TimeInMilliseconds(durationMetricName, float64(duration/time.Millisecond), []string{authorityTag, blockedTag}, 1)
}

type NullReporter struct{}

func (n NullReporter) Duration(request Request, blocked bool, duration time.Duration) error {
	return nil
}
