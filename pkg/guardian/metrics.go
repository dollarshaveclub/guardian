package guardian

import (
	"time"

	"github.com/DataDog/datadog-go/statsd"
)

type MetricReporter interface {
	Request(request Request) error
	Allowed(request Request) error
	Blocked(request Request) error
	Duration(request Request, duration time.Duration) error
}

type DataDogReporter struct {
	client *statsd.Client
}

const requestMetricName = "request.total"
const allowedMetricName = "request.allowed"
const blockedMetricName = "request.blocked"
const durationMetricName = "request.duration"

func (d *DataDogReporter) Request(request Request) error {
	return d.client.Incr(requestMetricName, []string{}, 1)
}

func (d *DataDogReporter) Allowed(request Request) error {
	return d.client.Incr(allowedMetricName, []string{}, 1)
}

func (d *DataDogReporter) Blocked(request Request) error {
	return d.client.Incr(blockedMetricName, []string{}, 1)
}

func (d *DataDogReporter) Duration(request Request, duration time.Duration) error {
	return d.client.Timing(durationMetricName, duration, []string{}, 1)
}

type NullReporter struct{}

func (n NullReporter) Request(request Request) error                          { return nil }
func (n NullReporter) Allowed(request Request) error                          { return nil }
func (n NullReporter) Blocked(request Request) error                          { return nil }
func (n NullReporter) Duration(request Request, duration time.Duration) error { return nil }
