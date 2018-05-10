package guardian

import (
	"net"
	"strconv"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/sirupsen/logrus"
)

const durationMetricName = "request.duration"
const reqWhitelistMetricName = "request.whitelist"
const reqRateLimitMetricName = "request.rate_limit"
const redisCounterIncrMetricName = "redis_counter.incr"
const redisCounterPrunedMetricName = "redis_counter.cache.pruned"
const redisCounterCacheSizeMetricName = "redis_counter.cache.size"
const redisCounterPrunePassMetricName = "redis_counter.cache.prune_pass"
const rateLimitCountMetricName = "rate_limit.count"
const rateLimitDurationMetricName = "rate_limit.duration"
const rateLimitEnabledMetricName = "rate_limit.enabled"
const whitelistCountMetricName = "whitelist.count"
const reportOnlyEnabledMetricName = "report_only.enabled"
const blockedKey = "blocked"
const whitelistedKey = "whitelisted"
const ratelimitedKey = "ratelimited"
const errorKey = "error"
const authorityKey = "authority"
const ingressClassKey = "ingress_class"

const metricChannelBuffSize = 1000000

type MetricReporter interface {
	Duration(request Request, blocked bool, errorOccurred bool, duration time.Duration)
	HandledWhitelist(request Request, whitelisted bool, errorOccurred bool, duration time.Duration)
	HandledRatelimit(request Request, ratelimited bool, errorOccurred bool, duration time.Duration)
	RedisCounterIncr(duration time.Duration, errorOccurred bool)
	RedisCounterPruned(duration time.Duration, cacheSize float64, prunedCounted float64)
	CurrentLimit(limit Limit)
	CurrentWhitelist(whitelist []net.IPNet)
	CurrentReportOnlyMode(reportOnly bool)
}

type DataDogReporter struct {
	client      *statsd.Client
	logger      logrus.FieldLogger
	defaultTags []string
	c           chan func()
}

func NewDataDogReporter(client *statsd.Client, defaultTags []string, logger logrus.FieldLogger) *DataDogReporter {
	return &DataDogReporter{
		client:      client,
		logger:      logger,
		defaultTags: defaultTags,
		c:           make(chan func(), metricChannelBuffSize),
	}
}

func (d *DataDogReporter) Run(stop <-chan struct{}) {
	for {
		select {
		case f := <-d.c:
			f()
		case <-stop:
			for f := range d.c { // drain the channel
				f()
			}
			return
		}
	}
}

func (d *DataDogReporter) Duration(request Request, blocked bool, errorOccurred bool, duration time.Duration) {
	f := func() {
		authorityTag := authorityKey + ":" + request.Authority
		blockedTag := blockedKey + ":" + strconv.FormatBool(blocked)
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		tags := append([]string{authorityTag, blockedTag, errorTag}, d.defaultTags...)
		d.client.TimeInMilliseconds(durationMetricName, float64(duration/time.Millisecond), tags, 1)
	}

	d.enqueue(f)
}

func (d *DataDogReporter) HandledWhitelist(request Request, whitelisted bool, errorOccurred bool, duration time.Duration) {
	f := func() {
		authorityTag := authorityKey + ":" + request.Authority
		whitelistedTag := whitelistedKey + ":" + strconv.FormatBool(whitelisted)
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		tags := append([]string{authorityTag, whitelistedTag, errorTag}, d.defaultTags...)
		d.client.TimeInMilliseconds(reqWhitelistMetricName, float64(duration/time.Millisecond), tags, 1.0)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) HandledRatelimit(request Request, ratelimited bool, errorOccurred bool, duration time.Duration) {
	f := func() {
		authorityTag := authorityKey + ":" + request.Authority
		ratelimitedTag := ratelimitedKey + ":" + strconv.FormatBool(ratelimited)
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		tags := append([]string{authorityTag, ratelimitedTag, errorTag}, d.defaultTags...)
		d.client.TimeInMilliseconds(reqRateLimitMetricName, float64(duration/time.Millisecond), tags, 1.0)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) RedisCounterIncr(duration time.Duration, errorOccurred bool) {
	f := func() {
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		tags := append([]string{errorTag}, d.defaultTags...)
		d.client.TimeInMilliseconds(redisCounterIncrMetricName, float64(duration/time.Millisecond), tags, 1.0)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) RedisCounterPruned(duration time.Duration, cacheSize float64, prunedCounted float64) {
	f := func() {
		d.client.Gauge(redisCounterCacheSizeMetricName, cacheSize, d.defaultTags, 1)
		d.client.Gauge(redisCounterPrunedMetricName, prunedCounted, d.defaultTags, 1)
		d.client.TimeInMilliseconds(redisCounterPrunePassMetricName, float64(duration/time.Millisecond), d.defaultTags, 1)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) CurrentLimit(limit Limit) {
	f := func() {
		enabled := 0
		if limit.Enabled {
			enabled = 1
		}

		d.client.Gauge(rateLimitCountMetricName, float64(limit.Count), d.defaultTags, 1)
		d.client.Gauge(rateLimitDurationMetricName, float64(limit.Duration), d.defaultTags, 1)
		d.client.Gauge(rateLimitEnabledMetricName, float64(enabled), d.defaultTags, 1)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) CurrentWhitelist(whitelist []net.IPNet) {
	f := func() {
		d.client.Gauge(whitelistCountMetricName, float64(len(whitelist)), d.defaultTags, 1)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) CurrentReportOnlyMode(reportOnly bool) {
	f := func() {
		enabled := 0
		if reportOnly {
			enabled = 1
		}
		d.client.Gauge(reportOnlyEnabledMetricName, float64(enabled), d.defaultTags, 1)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) enqueue(f func()) {
	select {
	case d.c <- f:
	default:
		d.logger.Error("buffered channel full -- discarding metric")
	}
}

type NullReporter struct{}

func (n NullReporter) Duration(request Request, blocked bool, errorOccured bool, duration time.Duration) {
}

func (n NullReporter) HandledWhitelist(request Request, whitelisted bool, errorOccured bool, duration time.Duration) {
}

func (n NullReporter) HandledRatelimit(request Request, ratelimited bool, errorOccured bool, duration time.Duration) {
}

func (n NullReporter) RedisCounterIncr(duration time.Duration, errorOccurred bool) {
}
func (n NullReporter) RedisCounterPruned(duration time.Duration, cacheSize float64, prunedCounted float64) {
}

func (n NullReporter) CurrentLimit(limit Limit) {
}

func (n NullReporter) CurrentWhitelist(whitelist []net.IPNet) {
}

func (n NullReporter) CurrentReportOnlyMode(reportOnly bool) {
}
