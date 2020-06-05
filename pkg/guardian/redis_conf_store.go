package guardian

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/bsm/redislock"
	"github.com/davecgh/go-spew/spew"
	"github.com/go-redis/redis"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Config file format versions
const (
	GlobalRateLimitConfigVersion = "v0"
	GlobalSettingsConfigVersion  = "v0"
	RateLimitConfigVersion       = "v0"
	JailConfigVersion            = "v0"
)

// Redis keys
const (
	redisIPWhitelistKey           = "guardian_conf:whitelist"
	redisIPBlacklistKey           = "guardian_conf:blacklist"
	redisPrisonersKey             = "guardian_conf:prisoners"
	redisPrisonersLockKey         = "guardian_conf:prisoners_lock"
	redisGlobalRateLimitConfigKey = "guardian_conf:global_rate_limit"
	redisGlobalSettingsConfigKey  = "guardian_conf:global_settings"
	redisRateLimitsConfigKey      = "guardian_conf:rate_limits"
	redisJailsConfigKey           = "guardian_conf:jails"

	// The remaining keys are associated with the deprecated CLI config.

	// Corresponds to redisGlobalRateLimitConfigKey
	redisLimitCountKey         = "guardian_conf:limit_count"
	redisLimitDurationKey      = "guardian_conf:limit_duration"
	redisLimitEnabledKey       = "guardian_conf:limit_enabled"
	redisUseDeprecatedLimitKey = "guardian_conf:use_deprecated_limit"
	// Corresponds to redisGlobalSettingsConfigKey
	redisReportOnlyKey              = "guardian_conf:reportOnly"
	redisUseDeprecatedReportOnlyKey = "guardian_conf:use_deprecated_reportOnly"
	// Corresponds to redisRateLimitsConfigKey
	redisRouteRateLimitsEnabledKey       = "guardian_conf:route_limits:enabled"
	redisRouteRateLimitsDurationKey      = "guardian_conf:route_limits:duration"
	redisRouteRateLimitsCountKey         = "guardian_conf:route_limits:count"
	redisUseDeprecatedRouteRateLimitsKey = "guardian_conf:use_deprecated_route_limits"
	// Corresponds to redisJailsConfigKey
	redisJailLimitsEnabledKey  = "guardian_conf:jail:limits:enabled"
	redisJailLimitsDurationKey = "guardian_conf:jail:limits:duration"
	redisJailLimitsCountKey    = "guardian_conf:jail:limits:count"
	redisJailBanDurationKey    = "guardian_conf:jail:ban:duration"
	redisUseDeprecatedJailsKey = "guardian_conf:use_deprecated_jails"
)

// NewRedisConfStore creates a new RedisConfStore
func NewRedisConfStore(redis *redis.Client, defaultWhitelist []net.IPNet, defaultBlacklist []net.IPNet, defaultLimit Limit, defaultReportOnly, initConfig bool, maxPrisonerCacheSize uint16, logger logrus.FieldLogger, mr MetricReporter) (*RedisConfStore, error) {
	if defaultWhitelist == nil {
		defaultWhitelist = []net.IPNet{}
	}

	if defaultBlacklist == nil {
		defaultBlacklist = []net.IPNet{}
	}

	if mr == nil {
		mr = NullReporter{}
	}

	locker := redislock.New(redis)
	prisoners, err := newPrisonerCache(maxPrisonerCacheSize)
	if err != nil {
		return nil, fmt.Errorf("unable to create prisoner cache: %v", err)
	}

	defaultConf := conf{
		whitelist: defaultWhitelist,
		blacklist: defaultBlacklist,
		globalRateLimit: GlobalRateLimitConfig{
			ConfigMetadata: ConfigMetadata{
				Version:     GlobalRateLimitConfigVersion,
				Kind:        GlobalRateLimitConfigKind,
				Name:        GlobalRateLimitConfigKind,
				Description: "",
			},
			Spec: GlobalRateLimitSpec{
				Limit: defaultLimit,
			},
		},
		globalSettings: GlobalSettingsConfig{
			ConfigMetadata: ConfigMetadata{
				Version:     GlobalSettingsConfigVersion,
				Kind:        GlobalSettingsConfigKind,
				Name:        GlobalSettingsConfigKind,
				Description: "",
			},
			Spec: GlobalSettingsSpec{
				ReportOnly: defaultReportOnly,
			},
		},
		rateLimitsByName: make(map[string]RateLimitConfig),
		rateLimitsByPath: make(map[string]RateLimitConfig),
		jailsByName:      make(map[string]JailConfig),
		jailsByPath:      make(map[string]JailConfig),
		// There are a couple limitations in redis that require us to need to keep track of prisoners and manage when they should expire.
		// 1. Redis Hashmaps and Sets to not support setting TTL of individual keys
		// 2. We could just rely upon unique keys in the global keyspace and do prefix lookups with the KEYS function, however, these lookups would be O(N).
		//    N in this case is every single entry within the database. This is not a valid option considering we could have millions of keys and do not want to
		//    block redis for too long.
		prisoners: prisoners,
		// Fields associated with deprecated CLI
		limit:                       defaultLimit,
		useDeprecatedLimit:          false,
		reportOnly:                  defaultReportOnly,
		useDeprecatedReportOnly:     false,
		routeRateLimits:             make(map[string]Limit),
		useDeprecatedRouteRateLimit: make(map[string]bool),
		useDeprecatedJail:           make(map[string]bool),
		jails:                       make(map[string]Jail),
	}

	rcf := &RedisConfStore{redis: redis, logger: logger, conf: &lockingConf{conf: defaultConf}, reporter: mr, locker: locker}
	if initConfig {
		if err := rcf.init(); err != nil {
			return nil, fmt.Errorf("unable to initialize RedisConfStore: %v", err)
		}
	}
	return rcf, nil
}

// RedisConfStore is a configuration provider that uses Redis for persistence
type RedisConfStore struct {
	redis    *redis.Client
	conf     *lockingConf
	logger   logrus.FieldLogger
	reporter MetricReporter
	locker   *redislock.Client
}

type conf struct {
	whitelist        []net.IPNet
	blacklist        []net.IPNet
	prisoners        *prisonersCache
	globalSettings   GlobalSettingsConfig
	globalRateLimit  GlobalRateLimitConfig
	rateLimitsByName map[string]RateLimitConfig
	rateLimitsByPath map[string]RateLimitConfig
	jailsByName      map[string]JailConfig
	jailsByPath      map[string]JailConfig
	// Fields associated with deprecated CLI
	limit                   Limit
	useDeprecatedLimit      bool
	reportOnly              bool
	useDeprecatedReportOnly bool
	// Map keys are request paths
	routeRateLimits             map[string]Limit
	useDeprecatedRouteRateLimit map[string]bool
	jails                       map[string]Jail
	useDeprecatedJail           map[string]bool
}

type lockingConf struct {
	sync.RWMutex
	conf
}

// ConfigKind identifies a kind of configuration resource
type ConfigKind string

// Config kinds
const (
	// GlobalRateLimitConfigKind identifies a Global Rate Limit config resource
	GlobalRateLimitConfigKind = "GlobalRateLimit"
	// RateLimitConfigKind identifies a Rate Limit config resource
	RateLimitConfigKind = "RateLimit"
	// JailConfigKind identifies a Jail config resource
	JailConfigKind = "Jail"
	// GlobalSettingsConfigKind identifies a Global Settings config resource
	GlobalSettingsConfigKind = "GlobalSettings"
)

// ConfigMetadata represents metadata associated with a configuration resource.
// Every configuration resource begins with this metadata.
type ConfigMetadata struct {
	// Version identifies the version of the configuration resource format
	Version string `yaml:"version" json:"version"`
	// Kind identifies the kind of the configuration resource
	Kind ConfigKind `yaml:"kind" json:"kind"`
	// Name uniquely identifies a configuration resource within the resource's Kind
	Name string `yaml:"name" json:"name"`
	// Description is a description to add context to a resource
	Description string `yaml:"description" json:"description"`
}

// Conditions represents conditions required for a Limit to be applied to
// a Request. Currently, Guardian only filters requests based on URL path,
// via RedisConfStore.GetRouteRateLimit(url.URL) or .GetJail(url.URL)
type Conditions struct {
	Path string `yaml:"path" json:"path"`
}

// GlobalRateLimitSpec represents the specification for a GlobalRateLimitConfig
type GlobalRateLimitSpec struct {
	Limit Limit `yaml:"limit" json:"limit"`
}

// GlobalSettingsSpec represents the specification for a GlobalSettingsConfig
type GlobalSettingsSpec struct {
	ReportOnly bool `yaml:"reportOnly" json:"reportOnly"`
}

// RateLimitSpec represents the specification for a RateLimitConfig
type RateLimitSpec struct {
	Limit      Limit      `yaml:"limit" json:"limit"`
	Conditions Conditions `yaml:"conditions" json:"conditions"`
}

// JailSpec represents the specification for a JailConfig
type JailSpec struct {
	Jail       `yaml:",inline" json:",inline"`
	Conditions Conditions `yaml:"conditions" json:"conditions"`
}

// GlobalRateLimitConfig represents a resource that configures the global rate limit
type GlobalRateLimitConfig struct {
	ConfigMetadata `yaml:",inline" json:",inline"`
	Spec           GlobalRateLimitSpec `yaml:"globalRateLimitSpec" json:"globalRateLimitSpec"`
}

// RateLimitConfig represents a resource that configures a conditional rate limit
type RateLimitConfig struct {
	ConfigMetadata `yaml:",inline" json:",inline"`
	Spec           RateLimitSpec `yaml:"rateLimitSpec" json:"rateLimitSpec"`
}

// JailConfig represents a resource that configures a jail
type JailConfig struct {
	ConfigMetadata `yaml:",inline" json:",inline"`
	Spec           JailSpec `yaml:"jailSpec" json:"jailSpec"`
}

// GlobalSettingsConfig represents a resource that configures global settings
type GlobalSettingsConfig struct {
	ConfigMetadata `yaml:",inline" json:",inline"`
	Spec           GlobalSettingsSpec `yaml:"globalSettingsSpec" json:"globalSettingsSpec"`
}

// JailConfigEntryDeprecated represents an entry in the jail configuration format
// associated with the deprecated CLI
type JailConfigEntryDeprecated struct {
	Route string `yaml:"route"`
	Jail  Jail   `yaml:"jail"`
}

// JailConfigEntryDeprecated represents the jail configuration format associated
// with the deprecated CLI
type JailConfigDeprecated struct {
	Jails []JailConfigEntryDeprecated `yaml:"jails"`
}

// RouteRateLimitConfigEntryDeprecated represents an entry in the conditional
// rate limit configuration format associated with the deprecated CLI
type RouteRateLimitConfigEntryDeprecated struct {
	Route string `yaml:"route"`
	Limit Limit  `yaml:"limit"`
}

// RouteRateLimitConfigDeprecated represents the conditional rate limit
// configuration format associated with the deprecated CLI
type RouteRateLimitConfigDeprecated struct {
	RouteRateLimits []RouteRateLimitConfigEntryDeprecated `yaml:"route_rate_limits"`
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

func (rs *RedisConfStore) IsPrisoner(remoteAddress string) bool {
	return rs.conf.prisoners.isPrisoner(remoteAddress)
}

func (rs *RedisConfStore) AddPrisoner(remoteAddress string, jail Jail) {
	p := rs.conf.prisoners.addPrisoner(remoteAddress, jail)
	go func() {
		value, err := json.Marshal(p)
		if err != nil {
			rs.logger.Errorf("error marshaling prisoner: %v", err)
		}

		lock, err := rs.obtainRedisPrisonersLock()
		if err != nil {
			rs.logger.Errorf("error obtaining lock to add prisoner: %v", err)
			return
		}

		defer rs.releaseRedisPrisonersLock(lock, time.Now().UTC())
		res := rs.redis.HSet(redisPrisonersKey, p.IP.String(), string(value))
		if res.Err() != nil {
			rs.logger.Errorf("error setting prisoner: key: %v, err: %v", redisPrisonersKey, p.IP.String, res.Err())
		}
	}()
}

// RemovePrisoners removes the specified prisoners from the prisoner hash map in Redis.
// This is intended to be used by the guardian-cli, so therefore it does not run rs.pipelinedFetchConf() or update the
// prisoners cache.
func (rs *RedisConfStore) RemovePrisoners(prisoners []net.IP) (numDeleted int64, err error) {
	if len(prisoners) == 0 {
		return int64(0), nil
	}
	p := []string{}
	for _, ip := range prisoners {
		p = append(p, ip.String())
	}
	lock, err := rs.obtainRedisPrisonersLock()
	if err != nil {
		rs.logger.Errorf("error obtaining lock to remove prisoners: %v", err)
		return
	}
	defer rs.releaseRedisPrisonersLock(lock, time.Now().UTC())
	cmd := rs.redis.HDel(redisPrisonersKey, p...)
	return cmd.Result()
}

func (rs *RedisConfStore) FetchPrisoners() ([]Prisoner, error) {
	lock, err := rs.obtainRedisPrisonersLock()
	if err != nil {
		return nil, fmt.Errorf("unable to obtain lock fetching prisoners: %v", err)
	}

	defer rs.releaseRedisPrisonersLock(lock, time.Now().UTC())
	prisonersCmd := rs.redis.HGetAll(redisPrisonersKey)
	prisonersRes, err := prisonersCmd.Result()
	if err != nil {
		return nil, fmt.Errorf("unable to fetch prisoners: %v", err)
	}
	prisoners := []Prisoner{}
	for _, prisonerJson := range prisonersRes {
		var prisoner Prisoner
		err := json.Unmarshal([]byte(prisonerJson), &prisoner)
		if err != nil {
			rs.logger.WithError(err).Warnf("unable to unmarshal json: %v", err)
			continue
		}
		if time.Now().UTC().Before(prisoner.Expiry) {
			prisoners = append(prisoners, prisoner)
		}
	}
	return prisoners, nil
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

func (rs *RedisConfStore) ApplyJailConfig(config JailConfig) error {
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("error marshaling json: %v", err)
	}
	pipe := rs.redis.TxPipeline()
	pipe.HSet(redisJailsConfigKey, config.Name, string(configBytes))
	pipe.HSet(redisUseDeprecatedJailsKey, config.Spec.Conditions.Path, "false")

	_, err = pipe.Exec()
	if err != nil {
		return err
	}
	return nil
}

func (rs *RedisConfStore) SetJailsDeprecated(jails map[string]Jail) error {
	for path, jail := range jails {
		limitCountStr := strconv.FormatUint(jail.Limit.Count, 10)
		limitDurationStr := jail.Limit.Duration.String()
		limitEnabledStr := strconv.FormatBool(jail.Limit.Enabled)
		jailBanDuration := jail.BanDuration.String()

		pipe := rs.redis.TxPipeline()
		pipe.HSet(redisJailLimitsCountKey, path, limitCountStr)
		pipe.HSet(redisJailLimitsDurationKey, path, limitDurationStr)
		pipe.HSet(redisJailLimitsEnabledKey, path, limitEnabledStr)
		pipe.HSet(redisJailBanDurationKey, path, jailBanDuration)
		pipe.HSet(redisUseDeprecatedJailsKey, path, "true")
		_, err := pipe.Exec()
		if err != nil {
			return err
		}
	}
	return nil
}

func (rs *RedisConfStore) DeleteJailConfig(name string) error {
	rs.logger.Debugf("Sending HDel for key %v field %v", redisJailsConfigKey, name)
	return rs.redis.HDel(redisJailsConfigKey, name).Err()
}

func (rs *RedisConfStore) RemoveJailsDeprecated(paths []string) error {
	for _, path := range paths {
		rs.logger.Debugf("Sending HDel for key %v field %v", redisJailLimitsCountKey, path)
		rs.logger.Debugf("Sending HDel for key %v field %v", redisJailLimitsDurationKey, path)
		rs.logger.Debugf("Sending HDel for key %v field %v", redisJailLimitsEnabledKey, path)
		rs.logger.Debugf("Sending HDel for key %v field %v", redisJailBanDurationKey, path)

		pipe := rs.redis.TxPipeline()
		pipe.HDel(redisJailLimitsCountKey, path)
		pipe.HDel(redisJailLimitsDurationKey, path)
		pipe.HDel(redisJailLimitsEnabledKey, path)
		pipe.HDel(redisJailBanDurationKey, path)
		_, err := pipe.Exec()
		if err != nil {
			return err
		}
	}
	return nil
}

func (rs *RedisConfStore) GetJail(url url.URL) Jail {
	rs.conf.RLock()
	defer rs.conf.RUnlock()
	if rs.conf.useDeprecatedJail[url.Path] {
		return rs.conf.jails[url.Path]
	}
	conf, ok := rs.conf.jailsByPath[url.Path]
	if !ok {
		return Jail{}
	}
	return conf.Spec.Jail
}

func (rs *RedisConfStore) FetchJailConfigs() []JailConfig {
	c := rs.pipelinedFetchConf()
	configs := make([]JailConfig, 0, len(c.jailsByName))
	for _, config := range c.jailsByName {
		configs = append(configs, config)
	}
	return configs
}

func (rs *RedisConfStore) FetchJailDeprecated(path string) (Jail, error) {
	c := rs.pipelinedFetchConf()
	count, _ := c.jailLimitCounts[path]
	duration, _ := c.jailLimitDurations[path]
	enabled, _ := c.jailLimitsEnabled[path]
	banDuration, _ := c.jailBanDuration[path]
	if count == nil || duration == nil || enabled == nil || banDuration == nil {
		return Jail{}, fmt.Errorf("jail not found")
	}
	return Jail{
		Limit: Limit{
			Count:    *count,
			Duration: *duration,
			Enabled:  *enabled,
		},
		BanDuration: *banDuration,
	}, nil
}

func (rs *RedisConfStore) FetchJailsDeprecated() (map[string]Jail, error) {
	c := rs.pipelinedFetchConf()
	jails := make(map[string]Jail)

	for path, count := range c.jailLimitCounts {
		duration, _ := c.jailLimitDurations[path]
		enabled, _ := c.jailLimitsEnabled[path]
		banDuration, _ := c.jailBanDuration[path]
		if count != nil && duration != nil && enabled != nil && banDuration != nil {
			jails[path] = Jail{
				Limit: Limit{
					Duration: *duration,
					Enabled:  *enabled,
					Count:    *count,
				},
				BanDuration: *banDuration,
			}
		}
	}
	return jails, nil
}

func (rs *RedisConfStore) ApplyRateLimitConfig(config RateLimitConfig) error {
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("error marshaling json: %v", err)
	}
	pipe := rs.redis.TxPipeline()
	pipe.HSet(redisRateLimitsConfigKey, config.Name, string(configBytes))
	pipe.HSet(redisUseDeprecatedRouteRateLimitsKey, config.Spec.Conditions.Path, "false")
	_, err = pipe.Exec()
	return err
}

// SetRouteRateLimitsDeprecated set the limit for each route.
// If the route limit is already defined in the store, it will be overwritten.
func (rs *RedisConfStore) SetRouteRateLimitsDeprecated(routeRateLimits map[string]Limit) error {
	for route, limit := range routeRateLimits {
		limitCountStr := strconv.FormatUint(limit.Count, 10)
		limitDurationStr := limit.Duration.String()
		limitEnabledStr := strconv.FormatBool(limit.Enabled)

		pipe := rs.redis.TxPipeline()
		pipe.HSet(redisRouteRateLimitsCountKey, route, limitCountStr)
		pipe.HSet(redisRouteRateLimitsDurationKey, route, limitDurationStr)
		pipe.HSet(redisRouteRateLimitsEnabledKey, route, limitEnabledStr)
		pipe.HSet(redisUseDeprecatedRouteRateLimitsKey, route, "true")
		_, err := pipe.Exec()
		if err != nil {
			return err
		}
	}
	return nil
}

func (rs *RedisConfStore) DeleteRateLimitConfig(name string) error {
	rs.logger.Debugf("Sending HDel for key %v field %v", redisJailsConfigKey, name)
	return rs.redis.HDel(redisRateLimitsConfigKey, name).Err()
}

// RemoveRouteRateLimitsDeprecated will iterate through the slice and delete the limit for each route.
// If one of the routes has not be set in the conf store, it will be treated by Redis as an empty hash and it will effectively be a no-op.
// This function will continue to iterate through the slice and delete the remaining routes contained in the slice.
func (rs *RedisConfStore) RemoveRouteRateLimitsDeprecated(paths []string) error {
	for _, path := range paths {
		rs.logger.Debugf("Sending HDel for key %v field %v", redisRouteRateLimitsCountKey, path)
		rs.logger.Debugf("Sending HDel for key %v field %v", redisRouteRateLimitsDurationKey, path)
		rs.logger.Debugf("Sending HDel for key %v field %v", redisRouteRateLimitsEnabledKey, path)

		pipe := rs.redis.TxPipeline()
		pipe.HDel(redisRouteRateLimitsCountKey, path)
		pipe.HDel(redisRouteRateLimitsDurationKey, path)
		pipe.HDel(redisRouteRateLimitsEnabledKey, path)
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
	if rs.conf.useDeprecatedRouteRateLimit[url.Path] {
		return rs.conf.routeRateLimits[url.Path]
	}
	conf, ok := rs.conf.rateLimitsByPath[url.Path]
	if !ok {
		return Limit{}
	}
	return conf.Spec.Limit
}

func (rs *RedisConfStore) FetchRateLimitConfigs() []RateLimitConfig {
	c := rs.pipelinedFetchConf()
	configs := make([]RateLimitConfig, 0, len(c.rateLimitsByName))
	for _, config := range c.rateLimitsByName {
		configs = append(configs, config)
	}
	return configs
}

// FetchRouteRateLimitDeprecated fetches the route limit from the conf store.
func (rs *RedisConfStore) FetchRouteRateLimitDeprecated(path string) (Limit, error) {
	c := rs.pipelinedFetchConf()
	count, _ := c.routeLimitCounts[path]
	duration, _ := c.routeLimitDurations[path]
	enabled, _ := c.routeRateLimitsEnabled[path]
	if count == nil || duration == nil || enabled == nil {
		return Limit{}, fmt.Errorf("route limit not found")
	}
	return Limit{Count: *count, Duration: *duration, Enabled: *enabled}, nil
}

// FetchRouteRateLimitsDeprecated fetches all of the route rate limits from the conf store.
func (rs *RedisConfStore) FetchRouteRateLimitsDeprecated() (map[string]Limit, error) {
	c := rs.pipelinedFetchConf()
	res := make(map[string]Limit)

	for path, count := range c.routeLimitCounts {
		duration, _ := c.routeLimitDurations[path]
		enabled, _ := c.routeRateLimitsEnabled[path]
		if duration != nil && enabled != nil && count != nil {
			res[path] = Limit{
				Count:    *count,
				Duration: *duration,
				Enabled:  *enabled,
			}
		}
	}
	return res, nil
}

func (rs *RedisConfStore) ApplyGlobalRateLimitConfig(config GlobalRateLimitConfig) error {
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("error marshaling json: %v", err)
	}
	pipe := rs.redis.TxPipeline()
	pipe.Set(redisGlobalRateLimitConfigKey, string(configBytes), 0)
	pipe.Set(redisUseDeprecatedLimitKey, "false", 0)

	_, err = pipe.Exec()
	return err
}

func (rs *RedisConfStore) GetLimit() Limit {
	rs.conf.RLock()
	defer rs.conf.RUnlock()
	if rs.conf.useDeprecatedLimit {
		return rs.conf.limit
	}
	return rs.conf.globalRateLimit.Spec.Limit
}

func (rs *RedisConfStore) FetchGlobalRateLimitConfig() (GlobalRateLimitConfig, error) {
	c := rs.pipelinedFetchConf()
	if c.globalRateLimit == nil {
		return GlobalRateLimitConfig{}, fmt.Errorf("error fetching global rate limit config")
	}
	return *c.globalRateLimit, nil
}

func (rs *RedisConfStore) FetchLimitDeprecated() (Limit, error) {
	c := rs.pipelinedFetchConf()
	if c.limitCount == nil || c.limitDuration == nil || c.limitEnabled == nil {
		return Limit{}, fmt.Errorf("error fetching limit")
	}

	return Limit{Count: *c.limitCount, Duration: *c.limitDuration, Enabled: *c.limitEnabled}, nil
}

func (rs *RedisConfStore) SetLimitDeprecated(limit Limit) error {
	limitCountStr := strconv.FormatUint(limit.Count, 10)
	limitDurationStr := limit.Duration.String()
	limitEnabledStr := strconv.FormatBool(limit.Enabled)

	pipe := rs.redis.TxPipeline()
	pipe.Set(redisLimitCountKey, limitCountStr, 0)
	pipe.Set(redisLimitDurationKey, limitDurationStr, 0)
	pipe.Set(redisLimitEnabledKey, limitEnabledStr, 0)
	pipe.Set(redisUseDeprecatedLimitKey, "true", 0)

	_, err := pipe.Exec()
	return err
}

func (rs *RedisConfStore) ApplyGlobalSettingsConfig(config GlobalSettingsConfig) error {
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("error marshaling json: %v", err)
	}
	pipe := rs.redis.TxPipeline()
	pipe.Set(redisGlobalSettingsConfigKey, string(configBytes), 0)
	pipe.Set(redisUseDeprecatedReportOnlyKey, "false", 0)

	_, err = pipe.Exec()
	return err
}

func (rs *RedisConfStore) GetReportOnly() bool {
	rs.conf.RLock()
	defer rs.conf.RUnlock()
	if rs.conf.useDeprecatedReportOnly {
		return rs.conf.reportOnly
	}
	return rs.conf.globalSettings.Spec.ReportOnly
}

func (rs *RedisConfStore) FetchGlobalSettingsConfig() (GlobalSettingsConfig, error) {
	c := rs.pipelinedFetchConf()
	if c.globalSettings == nil {
		return GlobalSettingsConfig{}, fmt.Errorf("error fetching global settings config")
	}
	return *c.globalSettings, nil
}

func (rs *RedisConfStore) FetchReportOnlyDeprecated() (bool, error) {
	c := rs.pipelinedFetchConf()
	if c.reportOnly == nil {
		return false, fmt.Errorf("error fetching report only flag")
	}

	return *c.reportOnly, nil
}

func (rs *RedisConfStore) SetReportOnlyDeprecated(reportOnly bool) error {
	reportOnlyStr := strconv.FormatBool(reportOnly)

	pipe := rs.redis.TxPipeline()
	pipe.Set(redisReportOnlyKey, reportOnlyStr, 0).Err()
	pipe.Set(redisUseDeprecatedReportOnlyKey, "true", 0)

	_, err := pipe.Exec()
	if err != nil {
		return err
	}
	return nil
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

// init creates configuration if missing from redis
func (rs *RedisConfStore) init() error {
	rs.logger.Debug("Initializing conf")
	rs.conf.RLock()
	defer rs.conf.RUnlock()

	if rs.redis.Get(redisIPWhitelistKey).Err() == redis.Nil {
		rs.logger.Debug("Initializing whitelist")
		if err := rs.AddWhitelistCidrs(rs.conf.whitelist); err != nil {
			return errors.Wrap(err, "error initializing whitelist")
		}
	}

	if rs.redis.Get(redisIPBlacklistKey).Err() == redis.Nil {
		rs.logger.Debug("Initializing blacklist")
		if err := rs.AddBlacklistCidrs(rs.conf.blacklist); err != nil {
			return errors.Wrap(err, "error initializing blacklist")
		}
	}

	if rs.redis.Get(redisGlobalRateLimitConfigKey).Err() == redis.Nil {
		if err := rs.ApplyGlobalRateLimitConfig(rs.conf.globalRateLimit); err != nil {
			return errors.Wrap(err, "error initializing global settings")
		}
	}

	if rs.redis.Get(redisGlobalSettingsConfigKey).Err() == redis.Nil {
		if err := rs.ApplyGlobalSettingsConfig(rs.conf.globalSettings); err != nil {
			return errors.Wrap(err, "error initializing global settings")
		}
	}

	if rs.redis.Get(redisRateLimitsConfigKey).Err() == redis.Nil {
		for _, config := range rs.conf.rateLimitsByName {
			if err := rs.ApplyRateLimitConfig(config); err != nil {
				return errors.Wrap(err, "error initializing rate limit")
			}
		}
	}

	if rs.redis.Get(redisJailsConfigKey).Err() == redis.Nil {
		for _, config := range rs.conf.jailsByName {
			if err := rs.ApplyJailConfig(config); err != nil {
				return errors.Wrap(err, "error initializing jail")
			}
		}
	}

	// Fields associated with deprecated CLI
	if rs.redis.Get(redisLimitEnabledKey).Err() == redis.Nil ||
		rs.redis.Get(redisLimitDurationKey).Err() == redis.Nil ||
		rs.redis.Get(redisLimitCountKey).Err() == redis.Nil {
		rs.logger.Debug("Initializing limit")
		if err := rs.SetLimitDeprecated(rs.conf.limit); err != nil {
			return errors.Wrap(err, "error initializing limit")
		}
	}

	if rs.redis.Get(redisReportOnlyKey).Err() == redis.Nil {
		rs.logger.Debug("Initializing report only")
		if err := rs.SetReportOnlyDeprecated(rs.conf.reportOnly); err != nil {
			return errors.Wrap(err, "error initializing report only")
		}
	}

	if rs.redis.Get(redisRouteRateLimitsEnabledKey).Err() == redis.Nil ||
		rs.redis.Get(redisRouteRateLimitsCountKey).Err() == redis.Nil ||
		rs.redis.Get(redisRouteRateLimitsDurationKey).Err() == redis.Nil {
		rs.logger.Debug("Initializing route rate limits")
		if err := rs.SetRouteRateLimitsDeprecated(rs.conf.routeRateLimits); err != nil {
			return errors.Wrap(err, "error initializing route rate limits")
		}
	}

	rs.conf.useDeprecatedLimit = false
	rs.conf.useDeprecatedReportOnly = false
	rs.conf.useDeprecatedRouteRateLimit = make(map[string]bool)
	rs.conf.useDeprecatedJail = make(map[string]bool)
	rs.logger.Debug("Success initializing conf")
	return nil
}

func (rs *RedisConfStore) UpdateCachedConf() {
	rs.logger.Debug("Updating conf")

	rs.logger.Debug("Fetching conf")
	fetched := rs.pipelinedFetchConf()
	rs.logger.Debugf("Fetched conf: %v", spew.Sdump(fetched))

	rs.conf.Lock()
	defer rs.conf.Unlock()

	if fetched.whitelist != nil {
		rs.conf.whitelist = fetched.whitelist
	}

	if fetched.blacklist != nil {
		rs.conf.blacklist = fetched.blacklist
	}

	if fetched.globalRateLimit != nil {
		rs.conf.globalRateLimit = *fetched.globalRateLimit
		rs.reporter.CurrentGlobalLimit(rs.conf.globalRateLimit.Spec.Limit)
	}

	if fetched.globalSettings != nil {
		rs.conf.globalSettings = *fetched.globalSettings
		rs.reporter.CurrentReportOnlyMode(rs.conf.globalSettings.Spec.ReportOnly)
	}

	rs.conf.rateLimitsByName = make(map[string]RateLimitConfig)
	rs.conf.rateLimitsByPath = make(map[string]RateLimitConfig)

	for name, config := range fetched.rateLimitsByName {
		rs.conf.rateLimitsByName[name] = config
		path := config.Spec.Conditions.Path
		rs.conf.rateLimitsByPath[path] = config
		rs.reporter.CurrentRouteLimit(path, config.Spec.Limit)
	}

	rs.conf.jailsByName = make(map[string]JailConfig)
	rs.conf.jailsByPath = make(map[string]JailConfig)

	for name, config := range fetched.jailsByName {
		rs.conf.jailsByName[name] = config
		path := config.Spec.Conditions.Path
		rs.conf.jailsByPath[path] = config
		rs.reporter.CurrentRouteLimit(path, config.Spec.Limit)
	}

	// Update deprecated fields
	if fetched.limitCount != nil &&
		fetched.limitDuration != nil &&
		fetched.limitEnabled != nil {
		rs.conf.limit.Count = *fetched.limitCount
		rs.conf.limit.Duration = *fetched.limitDuration
		rs.conf.limit.Enabled = *fetched.limitEnabled
	}

	if fetched.useDeprecatedLimit != nil {
		rs.conf.useDeprecatedLimit = *fetched.useDeprecatedLimit

		if *fetched.useDeprecatedLimit {
			rs.reporter.CurrentGlobalLimit(rs.conf.limit)
		}
	} else {
		rs.conf.useDeprecatedLimit = false
	}

	if fetched.reportOnly != nil {
		rs.conf.reportOnly = *fetched.reportOnly
	}

	if fetched.useDeprecatedReportOnly != nil {
		rs.conf.useDeprecatedReportOnly = *fetched.useDeprecatedReportOnly

		if *fetched.useDeprecatedReportOnly {
			rs.reporter.CurrentReportOnlyMode(rs.conf.reportOnly)
		}
	} else {
		rs.conf.useDeprecatedReportOnly = false
	}

	rs.conf.routeRateLimits = make(map[string]Limit, len(fetched.routeLimitCounts))

	for path, count := range fetched.routeLimitCounts {
		duration, _ := fetched.routeLimitDurations[path]
		enabled, _ := fetched.routeRateLimitsEnabled[path]
		if duration != nil && enabled != nil && count != nil {
			l := Limit{
				Count:    *count,
				Duration: *duration,
				Enabled:  *enabled,
			}
			rs.conf.routeRateLimits[path] = l
		}
	}

	rs.conf.useDeprecatedRouteRateLimit = make(map[string]bool, len(fetched.useDeprecatedRouteRateLimit))

	for path, useDeprecated := range fetched.useDeprecatedRouteRateLimit {
		if useDeprecated != nil {
			rs.conf.useDeprecatedRouteRateLimit[path] = *useDeprecated

			if *useDeprecated {
				rs.reporter.CurrentRouteLimit(path, rs.conf.routeRateLimits[path])
			}
		}
	}

	rs.conf.jails = make(map[string]Jail, len(fetched.jailLimitCounts))

	for path, count := range fetched.jailLimitCounts {
		duration, _ := fetched.jailLimitDurations[path]
		enabled, _ := fetched.jailLimitsEnabled[path]
		banDuration, _ := fetched.jailBanDuration[path]
		if duration != nil && enabled != nil && count != nil && banDuration != nil {
			j := Jail{
				Limit: Limit{
					Count:    *count,
					Duration: *duration,
					Enabled:  *enabled,
				},
				BanDuration: *banDuration,
			}
			rs.conf.jails[path] = j
		}
	}

	rs.conf.useDeprecatedJail = make(map[string]bool, len(fetched.useDeprecatedJail))

	for path, useDeprecated := range fetched.useDeprecatedJail {
		if useDeprecated != nil {
			rs.conf.useDeprecatedJail[path] = *useDeprecated

			if *useDeprecated {
				rs.reporter.CurrentRouteJail(path, rs.conf.jails[path])
			}
		}
	}

	rs.reporter.CurrentWhitelist(rs.conf.whitelist)
	rs.reporter.CurrentBlacklist(rs.conf.blacklist)
	rs.reporter.CurrentPrisoners(rs.conf.prisoners.length())
	rs.logger.Debug("Updated conf")
}

type fetchConf struct {
	whitelist        []net.IPNet
	blacklist        []net.IPNet
	globalRateLimit  *GlobalRateLimitConfig
	globalSettings   *GlobalSettingsConfig
	rateLimitsByName map[string]RateLimitConfig
	rateLimitsByPath map[string]RateLimitConfig
	jailsByName      map[string]JailConfig
	jailsByPath      map[string]JailConfig
	// Fields associated with the deprecated CLI
	limitCount                  *uint64
	limitDuration               *time.Duration
	limitEnabled                *bool
	useDeprecatedLimit          *bool
	reportOnly                  *bool
	useDeprecatedReportOnly     *bool
	routeLimitDurations         map[string]*time.Duration
	routeLimitCounts            map[string]*uint64
	routeRateLimitsEnabled      map[string]*bool
	useDeprecatedRouteRateLimit map[string]*bool
	jailLimitDurations          map[string]*time.Duration
	jailLimitCounts             map[string]*uint64
	jailLimitsEnabled           map[string]*bool
	jailBanDuration             map[string]*time.Duration
	useDeprecatedJail           map[string]*bool
}

func (rs *RedisConfStore) obtainRedisPrisonersLock() (*redislock.Lock, error) {
	expRetry := redislock.ExponentialBackoff(32*time.Millisecond, 128*time.Millisecond)
	// Default read/write timeout for the go-redis/redis client is 3 seconds.
	// The lock TTL should be greater than the read/write timeout.
	lock, err := rs.locker.Obtain(redisPrisonersLockKey, 6*time.Second, &redislock.Options{
		RetryStrategy: redislock.LimitRetry(expRetry, 5),
		Metadata:      "",
	})
	rs.reporter.RedisObtainLock(err != nil)
	return lock, err
}

func (rs *RedisConfStore) releaseRedisPrisonersLock(lock *redislock.Lock, start time.Time) {
	err := lock.Release()
	if err != nil {
		rs.logger.Errorf("error releasing lock: %v", err)
	}
	rs.reporter.RedisReleaseLock(time.Since(start), err != nil)
}

func (rs *RedisConfStore) pipelinedFetchConf() fetchConf {
	newConf := fetchConf{
		rateLimitsByName: make(map[string]RateLimitConfig),
		rateLimitsByPath: make(map[string]RateLimitConfig),
		jailsByName:      make(map[string]JailConfig),
		jailsByPath:      make(map[string]JailConfig),
		// Fields associated with the deprecated CLI
		routeLimitDurations:         make(map[string]*time.Duration),
		routeLimitCounts:            make(map[string]*uint64),
		routeRateLimitsEnabled:      make(map[string]*bool),
		useDeprecatedRouteRateLimit: make(map[string]*bool),
		jailLimitDurations:          make(map[string]*time.Duration),
		jailLimitCounts:             make(map[string]*uint64),
		jailLimitsEnabled:           make(map[string]*bool),
		jailBanDuration:             make(map[string]*time.Duration),
		useDeprecatedJail:           make(map[string]*bool),
	}

	rs.logger.Debugf("Sending GET for key %v", redisGlobalRateLimitConfigKey)
	rs.logger.Debugf("Sending GET for key %v", redisGlobalSettingsConfigKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisRateLimitsConfigKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisJailsConfigKey)
	rs.logger.Debugf("Sending HKEYS for key %v", redisIPWhitelistKey)
	rs.logger.Debugf("Sending HKEYS for key %v", redisIPBlacklistKey)
	rs.logger.Debugf("Sending GET for key %v", redisLimitCountKey)
	rs.logger.Debugf("Sending GET for key %v", redisLimitDurationKey)
	rs.logger.Debugf("Sending GET for key %v", redisLimitEnabledKey)
	rs.logger.Debugf("Sending GET for key %v", redisUseDeprecatedLimitKey)
	rs.logger.Debugf("Sending GET for key %v", redisReportOnlyKey)
	rs.logger.Debugf("Sending GET for key %v", redisUseDeprecatedReportOnlyKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisRouteRateLimitsCountKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisRouteRateLimitsDurationKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisRouteRateLimitsEnabledKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisUseDeprecatedRouteRateLimitsKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisJailLimitsCountKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisJailLimitsDurationKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisJailLimitsEnabledKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisJailBanDurationKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisUseDeprecatedJailsKey)

	pipe := rs.redis.Pipeline()
	defer pipe.Close()
	whitelistKeysCmd := pipe.HKeys(redisIPWhitelistKey)
	blacklistKeysCmd := pipe.HKeys(redisIPBlacklistKey)
	globalRateLimitCmd := pipe.Get(redisGlobalRateLimitConfigKey)
	globalSettingsCmd := pipe.Get(redisGlobalSettingsConfigKey)
	rateLimitsCmd := pipe.HGetAll(redisRateLimitsConfigKey)
	jailsCmd := pipe.HGetAll(redisJailsConfigKey)
	// Fields associated with the deprecated CLI
	limitCountCmd := pipe.Get(redisLimitCountKey)
	limitDurationCmd := pipe.Get(redisLimitDurationKey)
	limitEnabledCmd := pipe.Get(redisLimitEnabledKey)
	useDeprecatedLimitCmd := pipe.Get(redisUseDeprecatedLimitKey)
	reportOnlyCmd := pipe.Get(redisReportOnlyKey)
	useDeprecatedReportOnlyCmd := pipe.Get(redisUseDeprecatedReportOnlyKey)
	routeRateLimitsCountCmd := pipe.HGetAll(redisRouteRateLimitsCountKey)
	routeRateLimitsDurationCmd := pipe.HGetAll(redisRouteRateLimitsDurationKey)
	routeRateLimitsEnabledCmd := pipe.HGetAll(redisRouteRateLimitsEnabledKey)
	useDeprecatedRouteRateLimitCmd := pipe.HGetAll(redisUseDeprecatedRouteRateLimitsKey)
	jailLimitCountCmd := pipe.HGetAll(redisJailLimitsCountKey)
	jailLimitDurationCmd := pipe.HGetAll(redisJailLimitsDurationKey)
	jailLimitEnabledCmd := pipe.HGetAll(redisJailLimitsEnabledKey)
	jailBanDurationCmd := pipe.HGetAll(redisJailBanDurationKey)
	useDeprecatedJailCmd := pipe.HGetAll(redisUseDeprecatedJailsKey)

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

	if globalRateLimitBytes, err := globalRateLimitCmd.Bytes(); err == nil {
		config := GlobalRateLimitConfig{}
		if err := json.Unmarshal(globalRateLimitBytes, &config); err == nil {
			if config.Version == GlobalRateLimitConfigVersion {
				newConf.globalRateLimit = &config
			} else {
				rs.logger.Warnf(
					"stored global rate limit config version %v does not match current version %v; skipping",
					config.Version,
					GlobalRateLimitConfigVersion,
				)
			}
		} else {
			rs.logger.WithError(err).Warnf("error unmarshaling json for key %v", redisGlobalRateLimitConfigKey)
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Warnf("error sending GET for key %v", redisGlobalRateLimitConfigKey)
	}

	if globalSettingsBytes, err := globalSettingsCmd.Bytes(); err == nil {
		config := GlobalSettingsConfig{}
		if err := json.Unmarshal(globalSettingsBytes, &config); err == nil {
			if config.Version == GlobalSettingsConfigVersion {
				newConf.globalSettings = &config
			} else {
				rs.logger.Warnf(
					"stored global settings config version %v does not match current version %v; skipping",
					config.Version,
					GlobalSettingsConfigVersion,
				)
			}
		} else {
			rs.logger.WithError(err).Warnf("error unmarshaling json for key %v", redisGlobalSettingsConfigKey)
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Warnf("error sending GET for key %v", redisGlobalSettingsConfigKey)
	}

	for name, configString := range rateLimitsCmd.Val() {
		config := RateLimitConfig{}
		if err := json.Unmarshal([]byte(configString), &config); err == nil {
			if config.Version == RateLimitConfigVersion {
				newConf.rateLimitsByName[name] = config
				newConf.rateLimitsByPath[config.Spec.Conditions.Path] = config
			} else {
				rs.logger.Warnf(
					"stored rate limit config version %v does not match current version %v; skipping",
					config.Version,
					RateLimitConfigVersion,
				)
			}
		} else {
			rs.logger.WithError(err).Warnf("error unmarshaling json for key %v value %v", redisRateLimitsConfigKey, name)
		}
	}

	for name, configString := range jailsCmd.Val() {
		config := JailConfig{}
		if err := json.Unmarshal([]byte(configString), &config); err == nil {
			if config.Version == JailConfigVersion {
				newConf.jailsByName[name] = config
				newConf.jailsByPath[config.Spec.Conditions.Path] = config
			} else {
				rs.logger.Warnf(
					"stored jail config version %v does not match current version %v; skipping",
					config.Version,
					JailConfigVersion,
				)
			}
		} else {
			rs.logger.WithError(err).Warnf("error unmarshaling json for key %v value %v", redisJailsConfigKey, name)
		}
	}

	// Fields associated with deprecated CLI

	if limitCount, err := limitCountCmd.Uint64(); err == nil {
		newConf.limitCount = &limitCount
	} else if err != redis.Nil {
		rs.logger.WithError(err).Warnf("error sending GET for key %v", redisLimitCountKey)
	}

	if limitDurationStr, err := limitDurationCmd.Result(); err == nil {
		limitDuration, err := time.ParseDuration(limitDurationStr)
		if err != nil {
			rs.logger.WithError(err).Warnf("error parsing limit duration")
		} else {
			newConf.limitDuration = &limitDuration
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Errorf("error sending GET for key %v", redisLimitDurationKey)
	}

	if limitEnabledStr, err := limitEnabledCmd.Result(); err == nil {
		limitEnabled, err := strconv.ParseBool(limitEnabledStr)
		if err != nil {
			rs.logger.WithError(err).Warnf("error parsing limit enabled")
		} else {
			newConf.limitEnabled = &limitEnabled
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Errorf("error sending GET for key %v", redisLimitEnabledKey)
	}

	if useDeprecatedLimitStr, err := useDeprecatedLimitCmd.Result(); err == nil {
		useDeprecatedLimit, err := strconv.ParseBool(useDeprecatedLimitStr)
		if err != nil {
			rs.logger.WithError(err).Errorf("error sending GET for key %v", redisUseDeprecatedLimitKey)
		} else {
			newConf.useDeprecatedLimit = &useDeprecatedLimit
		}
	}

	if reportOnlyStr, err := reportOnlyCmd.Result(); err == nil {
		reportOnly, err := strconv.ParseBool(reportOnlyStr)
		if err != nil {
			rs.logger.WithError(err).Warnf("error parsing report only")
		} else {
			newConf.reportOnly = &reportOnly
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Warnf("error sending GET for key %v", redisReportOnlyKey)
	}

	if useDeprecatedReportOnlyStr, err := useDeprecatedReportOnlyCmd.Result(); err == nil {
		useDeprecatedReportOnly, err := strconv.ParseBool(useDeprecatedReportOnlyStr)
		if err != nil {
			rs.logger.WithError(err).Errorf("error sending GET for key %v", redisUseDeprecatedLimitKey)
		} else {
			newConf.useDeprecatedReportOnly = &useDeprecatedReportOnly
		}
	}

	if routeRateLimitsCounts, err := routeRateLimitsCountCmd.Result(); err == nil {
		for route, countStr := range routeRateLimitsCounts {
			count, err := strconv.ParseUint(countStr, 10, 64)
			if err != nil {
				rs.logger.WithError(err).Warnf("error parsing route limit count for %v", route)
			} else {
				newConf.routeLimitCounts[route] = &count
			}
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Warnf("error sending HGETALL for key %v", redisRouteRateLimitsCountKey)
	}

	if routeRateLimitsDurations, err := routeRateLimitsDurationCmd.Result(); err == nil {
		for route, durationStr := range routeRateLimitsDurations {
			duration, err := time.ParseDuration(durationStr)
			if err != nil {
				rs.logger.WithError(err).Warnf("error parsing route limit duration for %v", route)
			} else {
				newConf.routeLimitDurations[route] = &duration
			}
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Warnf("error sending HGETALL for key %v", redisRouteRateLimitsDurationKey)
	}

	if routeRateLimitsEnabled, err := routeRateLimitsEnabledCmd.Result(); err == nil {
		for route, enabled := range routeRateLimitsEnabled {
			enabled, err := strconv.ParseBool(enabled)
			if err != nil {
				rs.logger.WithError(err).Warnf("error parsing route limit enabled for %v", route)
			} else {
				newConf.routeRateLimitsEnabled[route] = &enabled
			}
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Warnf("error sending HGETALL for key %v", redisRouteRateLimitsEnabledKey)
	}

	if useDeprecatedRouteRateLimits, err := useDeprecatedRouteRateLimitCmd.Result(); err == nil {
		for route, useDeprecatedStr := range useDeprecatedRouteRateLimits {
			useDeprecated, err := strconv.ParseBool(useDeprecatedStr)
			if err != nil {
				rs.logger.WithError(err).Warnf("error parsing route limit use deprecated for %v", route)
			} else {
				newConf.useDeprecatedRouteRateLimit[route] = &useDeprecated
			}
		}
	}

	if jailLimitCounts, err := jailLimitCountCmd.Result(); err == nil {
		for route, countStr := range jailLimitCounts {
			count, err := strconv.ParseUint(countStr, 10, 64)
			if err != nil {
				rs.logger.WithError(err).Warnf("error parsing jail limit count for %v", route)
			} else {
				newConf.jailLimitCounts[route] = &count
			}
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Warnf("error sending HGETALL for key %v", redisJailLimitsCountKey)
	}

	if jailLimitDurations, err := jailLimitDurationCmd.Result(); err == nil {
		for route, durationStr := range jailLimitDurations {
			duration, err := time.ParseDuration(durationStr)
			if err != nil {
				rs.logger.WithError(err).Warnf("error parsing jail limit duration for %v", route)
			} else {
				newConf.jailLimitDurations[route] = &duration
			}
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Warnf("error sending HGETALL for key %v", redisJailLimitsDurationKey)
	}

	if jailLimitsEnabled, err := jailLimitEnabledCmd.Result(); err == nil {
		for route, enabled := range jailLimitsEnabled {
			enabled, err := strconv.ParseBool(enabled)
			if err != nil {
				rs.logger.WithError(err).Warnf("error parsing route limit enabled for %v", route)
			} else {
				newConf.jailLimitsEnabled[route] = &enabled
			}
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Warnf("error sending HGETALL for key %v", redisJailLimitsEnabledKey)
	}

	if jailBanDurations, err := jailBanDurationCmd.Result(); err == nil {
		for route, durationStr := range jailBanDurations {
			duration, err := time.ParseDuration(durationStr)
			if err != nil {
				rs.logger.WithError(err).Warnf("error parsing jail ban duration for %v", route)
			} else {
				newConf.jailBanDuration[route] = &duration
			}
		}
	} else if err != redis.Nil {
		rs.logger.WithError(err).Warnf("error sending HGETALL for key %v", redisJailLimitsEnabledKey)
	}

	if useDeprecatedJails, err := useDeprecatedJailCmd.Result(); err == nil {
		for route, useDeprecatedStr := range useDeprecatedJails {
			useDeprecated, err := strconv.ParseBool(useDeprecatedStr)
			if err != nil {
				rs.logger.WithError(err).Warnf("error parsing jail use deprecated for %v", route)
			} else {
				newConf.useDeprecatedJail[route] = &useDeprecated
			}
		}
	}

	lock, err := rs.obtainRedisPrisonersLock()
	if err != nil {
		rs.logger.Errorf("error obtaining lock in pipelined fetch: %v", err)
		return newConf
	}

	defer rs.releaseRedisPrisonersLock(lock, time.Now().UTC())
	expiredPrisoners := []string{}
	prisonersCmd := rs.redis.HGetAll(redisPrisonersKey)
	// Note: In order to match the rest of the configuration, we purposely purge the prisoners regardless of whether we
	// can connect or update the data in Redis. This way, Guardian continues to "fail open"
	rs.conf.prisoners.purge()
	if prisoners, err := prisonersCmd.Result(); err == nil {
		for ip, prisonerJson := range prisoners {
			var prisoner Prisoner
			err := json.Unmarshal([]byte(prisonerJson), &prisoner)
			if err != nil {
				rs.logger.WithError(err).Warnf("unable to unmarshal json: %v", err)
				continue
			}
			if time.Now().UTC().Before(prisoner.Expiry) {
				rs.logger.Debugf("adding %v to prisoners\n", prisoner.IP.String())
				rs.conf.prisoners.addPrisonerFromStore(prisoner)
			} else {
				rs.logger.Debugf("removing %v from prisoners\n", prisoner.IP.String())
				expiredPrisoners = append(expiredPrisoners, ip)
			}
		}
	} else {
		rs.logger.Errorf("error getting prisoners from redis: %v", err)
	}

	if len(expiredPrisoners) > 0 {
		removeExpiredPrisonersCmd := rs.redis.HDel(redisPrisonersKey, expiredPrisoners...)
		if n, err := removeExpiredPrisonersCmd.Result(); err == nil {
			rs.logger.Debugf("removed %d expired prisoners: %v", n, expiredPrisoners)
		} else {
			rs.logger.Errorf("error removing expired prisoners: %v", err)
		}
	}

	return newConf
}
