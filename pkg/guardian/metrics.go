package guardian

import (
	"fmt"
	"net"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	multierror "github.com/hashicorp/go-multierror"
)

type MetricReporter interface {
	Duration(request Request, blocked bool, errorOccured bool, duration time.Duration) error
	CurrentLimit(limit Limit) error
	CurrentWhitelist(whitelist []net.IPNet) error
	CurrentReportOnlyMode(reportOnly bool) error
}

type DataDogReporter struct {
	Client      *statsd.Client
	DefaultTags []string
}

const durationMetricName = "request.duration"
const rateLimitCountMetricName = "rate_limit.count"
const rateLimitDurationMetricName = "rate_limit.duration"
const rateLimitEnabledMetricName = "rate_limit.enabled"
const whitelistCountMetricName = "whitelist.count"
const reportOnlyEnabledMetricName = "report_only.enabled"
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

func (d *DataDogReporter) CurrentLimit(limit Limit) error {
	enabled := 0
	if limit.Enabled {
		enabled = 1
	}

	errC := d.Client.Gauge(rateLimitCountMetricName, float64(limit.Count), d.DefaultTags, 1)
	errD := d.Client.Gauge(rateLimitDurationMetricName, float64(limit.Duration), d.DefaultTags, 1)
	errE := d.Client.Gauge(rateLimitEnabledMetricName, float64(enabled), d.DefaultTags, 1)

	return multierror.Append(errC, errD, errE).ErrorOrNil()
}

func (d *DataDogReporter) CurrentWhitelist(whitelist []net.IPNet) error {
	return d.Client.Gauge(whitelistCountMetricName, float64(len(whitelist)), d.DefaultTags, 1)
}

func (d *DataDogReporter) CurrentReportOnlyMode(reportOnly bool) error {
	enabled := 0
	if reportOnly {
		enabled = 1
	}

	return d.Client.Gauge(reportOnlyEnabledMetricName, float64(enabled), d.DefaultTags, 1)
}

type NullReporter struct{}

func (n NullReporter) Duration(request Request, blocked bool, errorOccured bool, duration time.Duration) error {
	return nil
}
func (n NullReporter) CurrentLimit(limit Limit) error {
	return nil
}

func (n NullReporter) CurrentWhitelist(whitelist []net.IPNet) error {
	return nil
}

func (n NullReporter) CurrentReportOnlyMode(reportOnly bool) error {
	return nil
}
