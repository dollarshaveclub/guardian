package guardian

import (
	"fmt"
	"time"

	"github.com/DataDog/datadog-go/statsd"
)

type MetricReporter interface {
	Duration(request Request, blocked bool, errorOccured bool, duration time.Duration) error
}

type DataDogReporter struct {
	Client      *statsd.Client
	DefaultTags []string
}

const durationMetricName = "request.duration"
const blockedKey = "blocked"
const errorKey = "error"
const authorityKey = "authority"
const ingressClassKey = "ingress_class"

func (d *DataDogReporter) Duration(request Request, blocked bool, errorOccured bool, duration time.Duration) error {
	authorityTag := fmt.Sprintf("%v:%v", authorityKey, request.Authority)
	blockedTag := fmt.Sprintf("%v:%v", blockedKey, blocked)
	errorTag := fmt.Sprintf("%v:%v", errorKey, errorOccured)
	tags := append([]string{authorityTag, blockedTag, errorTag}, d.DefaultTags...)
	return d.Client.TimeInMilliseconds(durationMetricName, float64(duration/time.Millisecond), tags, 1)
}

type NullReporter struct{}

func (n NullReporter) Duration(request Request, blocked bool, errorOccured bool, duration time.Duration) error {
	return nil
}
