package guardian

import (
	"net"
	"strconv"
	"time"

	"github.com/DataDog/datadog-go/statsd"
)

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
	Client      *statsd.Client
	DefaultTags []string
}

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

func (d *DataDogReporter) Duration(request Request, blocked bool, errorOccurred bool, duration time.Duration) {
	authorityTag := authorityKey + ":" + request.Authority
	blockedTag := blockedKey + ":" + strconv.FormatBool(blocked)
	errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
	tags := append([]string{authorityTag, blockedTag, errorTag}, d.DefaultTags...)
	go d.Client.TimeInMilliseconds(durationMetricName, float64(duration/time.Millisecond), tags, 1)
}

func (d *DataDogReporter) HandledWhitelist(request Request, whitelisted bool, errorOccurred bool, duration time.Duration) {
	authorityTag := authorityKey + ":" + request.Authority
	whitelistedTag := whitelistedKey + ":" + strconv.FormatBool(whitelisted)
	errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
	tags := append([]string{authorityTag, whitelistedTag, errorTag}, d.DefaultTags...)
	go d.Client.TimeInMilliseconds(reqWhitelistMetricName, float64(duration/time.Millisecond), tags, 1.0)
}

func (d *DataDogReporter) HandledRatelimit(request Request, ratelimited bool, errorOccurred bool, duration time.Duration) {
	authorityTag := authorityKey + ":" + request.Authority
	ratelimitedTag := ratelimitedKey + ":" + strconv.FormatBool(ratelimited)
	errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
	tags := append([]string{authorityTag, ratelimitedTag, errorTag}, d.DefaultTags...)
	go d.Client.TimeInMilliseconds(reqRateLimitMetricName, float64(duration/time.Millisecond), tags, 1.0)
}

func (d *DataDogReporter) RedisCounterIncr(duration time.Duration, errorOccurred bool) {
	errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
	tags := append([]string{errorTag}, d.DefaultTags...)
	go d.Client.TimeInMilliseconds(redisCounterIncrMetricName, float64(duration/time.Millisecond), tags, 1.0)
}

func (d *DataDogReporter) RedisCounterPruned(duration time.Duration, cacheSize float64, prunedCounted float64) {
	go d.Client.Gauge(redisCounterCacheSizeMetricName, cacheSize, d.DefaultTags, 1)
	go d.Client.Gauge(redisCounterPrunedMetricName, prunedCounted, d.DefaultTags, 1)
	go d.Client.TimeInMilliseconds(redisCounterPrunePassMetricName, float64(duration/time.Millisecond), d.DefaultTags, 1)
}

func (d *DataDogReporter) CurrentLimit(limit Limit) {
	enabled := 0
	if limit.Enabled {
		enabled = 1
	}

	go d.Client.Gauge(rateLimitCountMetricName, float64(limit.Count), d.DefaultTags, 1)
	go d.Client.Gauge(rateLimitDurationMetricName, float64(limit.Duration), d.DefaultTags, 1)
	go d.Client.Gauge(rateLimitEnabledMetricName, float64(enabled), d.DefaultTags, 1)
}

func (d *DataDogReporter) CurrentWhitelist(whitelist []net.IPNet) {
	go d.Client.Gauge(whitelistCountMetricName, float64(len(whitelist)), d.DefaultTags, 1)
}

func (d *DataDogReporter) CurrentReportOnlyMode(reportOnly bool) {
	enabled := 0
	if reportOnly {
		enabled = 1
	}
	go d.Client.Gauge(reportOnlyEnabledMetricName, float64(enabled), d.DefaultTags, 1)
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
