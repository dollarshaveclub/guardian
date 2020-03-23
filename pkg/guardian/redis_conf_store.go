package guardian

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/sirupsen/logrus"
)

const redisIPWhitelistKey = "guardian_conf:whitelist"
const redisIPBlacklistKey = "guardian_conf:blacklist"
const redisLimitCountKey = "guardian_conf:limit_count"
const redisLimitDurationKey = "guardian_conf:limit_duration"
const redisLimitEnabledKey = "guardian_conf:limit_enabled"
const redisReportOnlyKey = "guardian_conf:reportOnly"
const redisRouteRateLimitsEnabledKey = "guardian_conf:route_limits:enabled"
const redisRouteRateLimitsDurationKey = "guardian_conf:route_limits:duration"
const redisRouteRateLimitsCountKey = "guardian_conf:route_limits:count"

// NewRedisConfStore creates a new RedisConfStore
func NewRedisConfStore(redis *redis.Client, defaultWhitelist []net.IPNet, defaultBlacklist []net.IPNet, defaultLimit Limit, defaultReportOnly bool, logger logrus.FieldLogger, mr MetricReporter) *RedisConfStore {
	if defaultWhitelist == nil {
		defaultWhitelist = []net.IPNet{}
	}

	if defaultBlacklist == nil {
		defaultBlacklist = []net.IPNet{}
	}

	if mr == nil {
		mr = NullReporter{}
	}

	defaultConf := conf{
		whitelist:       defaultWhitelist,
		blacklist:       defaultBlacklist,
		limit:           defaultLimit,
		reportOnly:      defaultReportOnly,
		routeRateLimits: make(map[url.URL]Limit),
	}
	return &RedisConfStore{redis: redis, logger: logger, conf: &lockingConf{conf: defaultConf}, reporter: mr,}
}

// RedisConfStore is a configuration provider that uses Redis for persistence
type RedisConfStore struct {
	redis    *redis.Client
	conf     *lockingConf
	logger   logrus.FieldLogger
	reporter MetricReporter
}

type conf struct {
	whitelist       []net.IPNet
	blacklist       []net.IPNet
	limit           Limit
	reportOnly      bool
	routeRateLimits map[url.URL]Limit
}

type lockingConf struct {
	sync.RWMutex
	conf
}

// RouteRateLimitConfigEntry models an entry in a config file for adding route rate limits.
type RouteRateLimitConfigEntry struct {
	Route string `yaml:"route"`
	Limit Limit  `yaml:"limit"`
}

// RouteRateLimitConfig models the config file for adding route rate limits.
type RouteRateLimitConfig struct {
	RouteRatelimits []RouteRateLimitConfigEntry `yaml:"route_rate_limits"`
}

func (rs *RedisConfStore) GetWhitelist() []net.IPNet {
	rs.conf.RLock()
	defer rs.conf.RUnlock()

	return append([]net.IPNet{}, rs.conf.whitelist...)
}

func (rs *RedisConfStore) FetchWhitelist() ([]net.IPNet, error) {
	c := rs.pipelinedFetchConf()
	if c.whitelist == nil {
		return nil, fmt.Errorf("error fetching whitelist")
	}

	return c.whitelist, nil
}

func (rs *RedisConfStore) GetBlacklist() []net.IPNet {
	rs.conf.RLock()
	defer rs.conf.RUnlock()

	return append([]net.IPNet{}, rs.conf.blacklist...)
}

func (rs *RedisConfStore) FetchBlacklist() ([]net.IPNet, error) {
	c := rs.pipelinedFetchConf()
	if c.blacklist == nil {
		return nil, fmt.Errorf("error fetching blacklist")
	}

	return c.blacklist, nil
}

func (rs *RedisConfStore) AddWhitelistCidrs(cidrs []net.IPNet) error {
	key := redisIPWhitelistKey
	for _, cidr := range cidrs {
		field := cidr.String()
		rs.logger.Debugf("Sending HSet for key %v field %v", key, field)
		res := rs.redis.HSet(key, field, "true") // value doesn't matter

		if res.Err() != nil {
			return res.Err()
		}
	}

	return nil
}

func (rs *RedisConfStore) RemoveWhitelistCidrs(cidrs []net.IPNet) error {
	key := redisIPWhitelistKey
	for _, cidr := range cidrs {
		field := cidr.String()
		rs.logger.Debugf("Sending HDel for key %v field %v", key, field)
		res := rs.redis.HDel(key, field, "true") // value doesn't matter

		if res.Err() != nil {
			return res.Err()
		}
	}

	return nil
}

func (rs *RedisConfStore) AddBlacklistCidrs(cidrs []net.IPNet) error {
	key := redisIPBlacklistKey
	for _, cidr := range cidrs {
		field := cidr.String()
		rs.logger.Debugf("Sending HSet for key %v field %v", key, field)
		res := rs.redis.HSet(key, field, "true") // value doesn't matter

		if res.Err() != nil {
			return res.Err()
		}
	}

	return nil
}

func (rs *RedisConfStore) RemoveBlacklistCidrs(cidrs []net.IPNet) error {
	key := redisIPBlacklistKey
	for _, cidr := range cidrs {
		field := cidr.String()
		rs.logger.Debugf("Sending HDel for key %v field %v", key, field)
		res := rs.redis.HDel(key, field, "true") // value doesn't matter

		if res.Err() != nil {
			return res.Err()
		}
	}

	return nil
}

// SetRouteRateLimits set the limit for each route.
// If the route limit is already defined in the store, it will be overwritten.
func (rs *RedisConfStore) SetRouteRateLimits(routeRateLimits map[url.URL]Limit) error {
	for url, limit := range routeRateLimits {
		route := url.EscapedPath()
		limitCountStr := strconv.FormatUint(limit.Count, 10)
		limitDurationStr := limit.Duration.String()
		limitEnabledStr := strconv.FormatBool(limit.Enabled)

		pipe := rs.redis.TxPipeline()
		pipe.HSet(redisRouteRateLimitsCountKey, route, limitCountStr)
		pipe.HSet(redisRouteRateLimitsDurationKey, route, limitDurationStr)
		pipe.HSet(redisRouteRateLimitsEnabledKey, route, limitEnabledStr)
		_, err := pipe.Exec()
		if err != nil {
			return err
		}
	}
	return nil
}

// RemoveRouteRateLimits will iterate through the slice and delete the limit for each route.
// If one of the routes has not be set in the conf store, it will be treated by Redis as an empty hash and it will effectively be a no-op.
// This function will continue to iterate through the slice and delete the remaining routes contained in the slice.
func (rs *RedisConfStore) RemoveRouteRateLimits(urls []url.URL) error {
	for _, url := range urls {
		route := url.EscapedPath()
		rs.logger.Debugf("Sending HDel for key %v field %v", redisRouteRateLimitsCountKey, route)
		rs.logger.Debugf("Sending HDel for key %v field %v", redisRouteRateLimitsDurationKey, route)
		rs.logger.Debugf("Sending HDel for key %v field %v", redisRouteRateLimitsEnabledKey, route)

		pipe := rs.redis.TxPipeline()
		pipe.HDel(redisRouteRateLimitsCountKey, route)
		pipe.HDel(redisRouteRateLimitsDurationKey, route)
		pipe.HDel(redisRouteRateLimitsEnabledKey, route)
		_, err := pipe.Exec()
		if err != nil {
			return err
		}
	}
	return nil
}

// GetRouteRateLimit gets a route limit from the local cache.
func (rs *RedisConfStore) GetRouteRateLimit(url url.URL) Limit {
	rs.conf.RLock()
	defer rs.conf.RUnlock()
	limit, _ := rs.conf.routeRateLimits[url]
	return limit
}

// FetchRouteRateLimit fetches the route limit from the conf store.
func (rs *RedisConfStore) FetchRouteRateLimit(url url.URL) (Limit, error) {
	c := rs.pipelinedFetchConf()
	count, _ := c.routeLimitCounts[url]
	duration, _ := c.routeLimitDurations[url]
	enabled, _ := c.routeRateLimitsEnabled[url]
	if count == nil || duration == nil || enabled == nil {
		return Limit{}, fmt.Errorf("route limit not found")
	}
	return Limit{Count: *count, Duration: *duration, Enabled: *enabled}, nil
}

// FetchRouteRateLimits fetches all of the route rate limits from the conf store.
func (rs *RedisConfStore) FetchRouteRateLimits() (map[url.URL]Limit, error) {
	c := rs.pipelinedFetchConf()
	res := make(map[url.URL]Limit)

	for url, count := range c.routeLimitCounts {
		duration, _ := c.routeLimitDurations[url]
		enabled, _ := c.routeRateLimitsEnabled[url]
		if duration != nil && enabled != nil && count != nil {
			res[url] = Limit{
				Count:    *count,
				Duration: *duration,
				Enabled:  *enabled,
			}
		}
	}
	return res, nil
}

func (rs *RedisConfStore) GetLimit() Limit {
	rs.conf.RLock()
	defer rs.conf.RUnlock()

	return rs.conf.limit
}

func (rs *RedisConfStore) FetchLimit() (Limit, error) {
	c := rs.pipelinedFetchConf()
	if c.limitCount == nil || c.limitDuration == nil || c.limitEnabled == nil {
		return Limit{}, fmt.Errorf("error fetching limit")
	}

	return Limit{Count: *c.limitCount, Duration: *c.limitDuration, Enabled: *c.limitEnabled}, nil
}

func (rs *RedisConfStore) SetLimit(limit Limit) error {
	limitCountStr := strconv.FormatUint(limit.Count, 10)
	limitDurationStr := limit.Duration.String()
	limitEnabledStr := strconv.FormatBool(limit.Enabled)

	pipe := rs.redis.TxPipeline()
	pipe.Set(redisLimitCountKey, limitCountStr, 0)
	pipe.Set(redisLimitDurationKey, limitDurationStr, 0)
	pipe.Set(redisLimitEnabledKey, limitEnabledStr, 0)

	_, err := pipe.Exec()

	return err
}

func (rs *RedisConfStore) GetReportOnly() bool {
	rs.conf.RLock()
	defer rs.conf.RUnlock()

	return rs.conf.reportOnly
}

func (rs *RedisConfStore) FetchReportOnly() (bool, error) {
	c := rs.pipelinedFetchConf()
	if c.reportOnly == nil {
		return false, fmt.Errorf("error fetching report only flag")
	}

	return *c.reportOnly, nil
}

func (rs *RedisConfStore) SetReportOnly(reportOnly bool) error {
	reportOnlyStr := strconv.FormatBool(reportOnly)
	return rs.redis.Set(redisReportOnlyKey, reportOnlyStr, 0).Err()
}

func (rs *RedisConfStore) RunSync(updateInterval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(updateInterval)
	for {
		select {
		case <-ticker.C:
			rs.UpdateCachedConf()
		case <-stop:
			ticker.Stop()
			return
		}
	}
}

func (rs *RedisConfStore) UpdateCachedConf() {
	rs.logger.Debug("Updating conf")

	rs.logger.Debug("Fetching conf")
	fetched := rs.pipelinedFetchConf()
	rs.logger.Debugf("Fetched conf: %#v", fetched)

	rs.conf.Lock()
	defer rs.conf.Unlock()

	if fetched.whitelist != nil {
		rs.conf.whitelist = fetched.whitelist
	}

	if fetched.blacklist != nil {
		rs.conf.blacklist = fetched.blacklist
	}

	if fetched.limitCount != nil &&
		fetched.limitDuration != nil &&
		fetched.limitEnabled != nil {
		rs.conf.limit.Count = *fetched.limitCount
		rs.conf.limit.Duration = *fetched.limitDuration
		rs.conf.limit.Enabled = *fetched.limitEnabled
	}

	if fetched.reportOnly != nil {
		rs.conf.reportOnly = *fetched.reportOnly
	}

	for url, count := range fetched.routeLimitCounts {
		duration, _ := fetched.routeLimitDurations[url]
		enabled, _ := fetched.routeRateLimitsEnabled[url]
		if duration != nil && enabled != nil && count != nil {
			l := Limit{
				Count:    *count,
				Duration: *duration,
				Enabled:  *enabled,
			}
			rs.conf.routeRateLimits[url] = l
			rs.reporter.CurrentRouteLimit(url.Path, l)
		}
	}

	rs.reporter.CurrentGlobalLimit(rs.conf.limit)
	rs.reporter.CurrentWhitelist(rs.conf.whitelist)
	rs.reporter.CurrentBlacklist(rs.conf.blacklist)
	rs.reporter.CurrentReportOnlyMode(rs.conf.reportOnly)
	rs.logger.Debug("Updated conf")
}

type fetchConf struct {
	whitelist              []net.IPNet
	blacklist              []net.IPNet
	limitCount             *uint64
	limitDuration          *time.Duration
	limitEnabled           *bool
	reportOnly             *bool
	routeLimitDurations    map[url.URL]*time.Duration
	routeLimitCounts       map[url.URL]*uint64
	routeRateLimitsEnabled map[url.URL]*bool
}

func (rs *RedisConfStore) pipelinedFetchConf() fetchConf {
	newConf := fetchConf{
		routeLimitDurations:    make(map[url.URL]*time.Duration),
		routeLimitCounts:       make(map[url.URL]*uint64),
		routeRateLimitsEnabled: make(map[url.URL]*bool),
	}
	rs.logger.Debugf("Sending HKEYS for key %v", redisIPWhitelistKey)
	rs.logger.Debugf("Sending HKEYS for key %v", redisIPBlacklistKey)
	rs.logger.Debugf("Sending GET for key %v", redisLimitCountKey)
	rs.logger.Debugf("Sending GET for key %v", redisLimitDurationKey)
	rs.logger.Debugf("Sending GET for key %v", redisLimitEnabledKey)
	rs.logger.Debugf("Sending GET for key %v", redisReportOnlyKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisRouteRateLimitsCountKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisRouteRateLimitsDurationKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisRouteRateLimitsEnabledKey)

	pipe := rs.redis.Pipeline()
	defer pipe.Close()
	whitelistKeysCmd := pipe.HKeys(redisIPWhitelistKey)
	blacklistKeysCmd := pipe.HKeys(redisIPBlacklistKey)
	limitCountCmd := pipe.Get(redisLimitCountKey)
	limitDurationCmd := pipe.Get(redisLimitDurationKey)
	limitEnabledCmd := pipe.Get(redisLimitEnabledKey)
	reportOnlyCmd := pipe.Get(redisReportOnlyKey)
	routeRateLimitsCountCmd := pipe.HGetAll(redisRouteRateLimitsCountKey)
	routeRateLimitsDurationCmd := pipe.HGetAll(redisRouteRateLimitsDurationKey)
	routeRateLimitsEnabledCmd := pipe.HGetAll(redisRouteRateLimitsEnabledKey)
	pipe.Exec()

	if whitelistStrs, err := whitelistKeysCmd.Result(); err == nil {
		newConf.whitelist = IPNetsFromStrings(whitelistStrs, rs.logger)
	} else {
		rs.logger.WithError(err).Warnf("error send HKEYS for key %v", redisIPWhitelistKey)
	}

	if blacklistStrs, err := blacklistKeysCmd.Result(); err == nil {
		newConf.blacklist = IPNetsFromStrings(blacklistStrs, rs.logger)
	} else {
		rs.logger.WithError(err).Warnf("error send HKEYS for key %v", redisIPWhitelistKey)
	}

	if limitCount, err := limitCountCmd.Uint64(); err == nil {
		newConf.limitCount = &limitCount
	} else {
		rs.logger.WithError(err).Warnf("error sending GET for key %v", redisLimitCountKey)
	}

	if limitDurationStr, err := limitDurationCmd.Result(); err == nil {
		limitDuration, err := time.ParseDuration(limitDurationStr)
		if err != nil {
			rs.logger.WithError(err).Warnf("error parsing limit duration")
		} else {
			newConf.limitDuration = &limitDuration
		}
	} else {
		rs.logger.WithError(err).Errorf("error sending GET for key %v", redisLimitDurationKey)
	}

	if limitEnabledStr, err := limitEnabledCmd.Result(); err == nil {
		limitEnabled, err := strconv.ParseBool(limitEnabledStr)
		if err != nil {
			rs.logger.WithError(err).Warnf("error parsing limit enabled")
		} else {
			newConf.limitEnabled = &limitEnabled
		}
	} else {
		rs.logger.WithError(err).Errorf("error sending GET for key %v", redisLimitEnabledKey)
	}

	if reportOnlyStr, err := reportOnlyCmd.Result(); err == nil {
		reportOnly, err := strconv.ParseBool(reportOnlyStr)
		if err != nil {
			rs.logger.WithError(err).Warnf("error parsing report only")
		} else {
			newConf.reportOnly = &reportOnly
		}
	} else {
		rs.logger.WithError(err).Warnf("error sending GET for key %v", redisReportOnlyKey)
	}

	if routeRateLimitsCounts, err := routeRateLimitsCountCmd.Result(); err == nil {
		for route, countStr := range routeRateLimitsCounts {
			parsedURL, urlParseErr := url.Parse(route)
			count, intParseErr := strconv.ParseUint(countStr, 10, 64)
			if urlParseErr != nil || intParseErr != nil {
				rs.logger.WithError(urlParseErr).WithError(intParseErr).Warnf("error parsing route limit duration for %v", route)
			} else {
				newConf.routeLimitCounts[*parsedURL] = &count
			}
		}
	} else {
		rs.logger.WithError(err).Warnf("error sending HGETALL for key %v", redisRouteRateLimitsCountKey)
	}

	if routeRateLimitsDurations, err := routeRateLimitsDurationCmd.Result(); err == nil {
		for route, durationStr := range routeRateLimitsDurations {
			parsedURL, urlParseErr := url.Parse(route)
			duration, durationParseErr := time.ParseDuration(durationStr)
			if urlParseErr != nil || durationParseErr != nil {
				rs.logger.WithError(urlParseErr).WithError(durationParseErr).Warnf("error parsing route limit duration for %v", route)
			} else {
				newConf.routeLimitDurations[*parsedURL] = &duration
			}
		}
	} else {
		rs.logger.WithError(err).Warnf("error sending HGETALL for key %v", redisRouteRateLimitsDurationKey)
	}

	if routeRateLimitsEnabled, err := routeRateLimitsEnabledCmd.Result(); err == nil {
		for route, enabled := range routeRateLimitsEnabled {
			parsedURL, urlParseErr := url.Parse(route)
			enabled, boolParseErr := strconv.ParseBool(enabled)
			if urlParseErr != nil || boolParseErr != nil {
				rs.logger.WithError(urlParseErr).WithError(boolParseErr).Warnf("error parsing route limit enabled for %v", route)
			} else {
				newConf.routeRateLimitsEnabled[*parsedURL] = &enabled
			}
		}
	} else {
		rs.logger.WithError(err).Warnf("error sending HGETALL for key %v", redisRouteRateLimitsEnabledKey)
	}

	return newConf
}
