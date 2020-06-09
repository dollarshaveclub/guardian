package guardian

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
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
}

type lockingConf struct {
	sync.RWMutex
	conf
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

func (rs *RedisConfStore) ApplyJailConfig(cfg JailConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("error marshaling json: %v", err)
	}
	pipe := rs.redis.TxPipeline()
	pipe.HSet(redisJailsConfigKey, cfg.Name, string(b))

	_, err = pipe.Exec()
	if err != nil {
		return err
	}
	return nil
}

func (rs *RedisConfStore) DeleteJailConfig(name string) error {
	rs.logger.Debugf("Sending HDel for key %v field %v", redisJailsConfigKey, name)
	return rs.redis.HDel(redisJailsConfigKey, name).Err()
}

func (rs *RedisConfStore) GetJail(url url.URL) Jail {
	rs.conf.RLock()
	defer rs.conf.RUnlock()
	conf, ok := rs.conf.jailsByPath[url.Path]
	if !ok {
		return Jail{}
	}
	return conf.Spec.Jail
}

func (rs *RedisConfStore) FetchJailConfigs() []JailConfig {
	c := rs.pipelinedFetchConf()
	cfgs := make([]JailConfig, 0, len(c.jailsByName))
	for _, cfg := range c.jailsByName {
		cfgs = append(cfgs, cfg)
	}
	return cfgs
}

func (rs *RedisConfStore) FetchJailConfig(name string) (JailConfig, error) {
	c := rs.pipelinedFetchConf()
	cfg, ok := c.jailsByName[name]
	if !ok {
		return JailConfig{}, fmt.Errorf("no jail exists with name %v", name)
	}
	return cfg, nil
}

func (rs *RedisConfStore) ApplyRateLimitConfig(cfg RateLimitConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("error marshaling json: %v", err)
	}
	pipe := rs.redis.TxPipeline()
	pipe.HSet(redisRateLimitsConfigKey, cfg.Name, string(b))
	_, err = pipe.Exec()
	return err
}

func (rs *RedisConfStore) DeleteRateLimitConfig(name string) error {
	rs.logger.Debugf("Sending HDel for key %v field %v", redisJailsConfigKey, name)
	return rs.redis.HDel(redisRateLimitsConfigKey, name).Err()
}

// GetRouteRateLimit gets a route limit from the local cache.
func (rs *RedisConfStore) GetRouteRateLimit(url url.URL) Limit {
	rs.conf.RLock()
	defer rs.conf.RUnlock()
	conf, ok := rs.conf.rateLimitsByPath[url.Path]
	if !ok {
		return Limit{}
	}
	return conf.Spec.Limit
}

func (rs *RedisConfStore) FetchRateLimitConfigs() []RateLimitConfig {
	c := rs.pipelinedFetchConf()
	cfgs := make([]RateLimitConfig, 0, len(c.rateLimitsByName))
	for _, cfg := range c.rateLimitsByName {
		cfgs = append(cfgs, cfg)
	}
	return cfgs
}

func (rs *RedisConfStore) FetchRateLimitConfig(name string) (RateLimitConfig, error) {
	c := rs.pipelinedFetchConf()
	cfg, ok := c.rateLimitsByName[name]
	if !ok {
		return RateLimitConfig{}, fmt.Errorf("no rate limit exists with name %v", name)
	}
	return cfg, nil
}

func (rs *RedisConfStore) ApplyGlobalRateLimitConfig(cfg GlobalRateLimitConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("error marshaling json: %v", err)
	}
	pipe := rs.redis.TxPipeline()
	pipe.Set(redisGlobalRateLimitConfigKey, string(b), 0)
	_, err = pipe.Exec()
	return err
}

func (rs *RedisConfStore) GetLimit() Limit {
	rs.conf.RLock()
	defer rs.conf.RUnlock()
	return rs.conf.globalRateLimit.Spec.Limit
}

func (rs *RedisConfStore) FetchGlobalRateLimitConfig() (GlobalRateLimitConfig, error) {
	c := rs.pipelinedFetchConf()
	if c.globalRateLimit == nil {
		return GlobalRateLimitConfig{}, fmt.Errorf("error fetching global rate limit config")
	}
	return *c.globalRateLimit, nil
}

func (rs *RedisConfStore) ApplyGlobalSettingsConfig(cfg GlobalSettingsConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("error marshaling json: %v", err)
	}
	pipe := rs.redis.TxPipeline()
	pipe.Set(redisGlobalSettingsConfigKey, string(b), 0)

	_, err = pipe.Exec()
	return err
}

func (rs *RedisConfStore) GetReportOnly() bool {
	rs.conf.RLock()
	defer rs.conf.RUnlock()
	return rs.conf.globalSettings.Spec.ReportOnly
}

func (rs *RedisConfStore) FetchGlobalSettingsConfig() (GlobalSettingsConfig, error) {
	c := rs.pipelinedFetchConf()
	if c.globalSettings == nil {
		return GlobalSettingsConfig{}, fmt.Errorf("error fetching global settings config")
	}
	return *c.globalSettings, nil
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

	rs.conf.jailsByName = make(map[string]JailConfig, len(fetched.jailsByName))
	rs.conf.jailsByPath = make(map[string]JailConfig, len(fetched.jailsByName))

	for name, config := range fetched.jailsByName {
		rs.conf.jailsByName[name] = config
		path := config.Spec.Conditions.Path
		rs.conf.jailsByPath[path] = config
		rs.reporter.CurrentRouteLimit(path, config.Spec.Limit)
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

func (fc *fetchConf) setWhitelist(cmd *redis.StringSliceCmd, logger logrus.FieldLogger) {
	whitelistStrs, err := cmd.Result()
	if err != nil {
		logger.WithError(err).Warnf("error send HKEYS for key %v", redisIPWhitelistKey)
		return
	}
	fc.whitelist = IPNetsFromStrings(whitelistStrs, logger)
}

func (fc *fetchConf) setBlacklist(cmd *redis.StringSliceCmd, logger logrus.FieldLogger) {
	blacklistStrs, err := cmd.Result()
	if err != nil {
		logger.WithError(err).Warnf("error send HKEYS for key %v", redisIPWhitelistKey)
		return
	}
	fc.blacklist = IPNetsFromStrings(blacklistStrs, logger)
}

func (fc *fetchConf) setGlobalRateLimit(cmd *redis.StringCmd, logger logrus.FieldLogger) {
	b, err := cmd.Bytes()
	if err != nil {
		if err != redis.Nil {
			logger.WithError(err).Warnf("error sending GET for key %v", redisGlobalRateLimitConfigKey)
		}
		return
	}
	cfg := GlobalRateLimitConfig{}
	if err := json.Unmarshal(b, &cfg); err != nil {
		logger.WithError(err).Warnf("error unmarshaling json for key %v", redisGlobalRateLimitConfigKey)
		return
	}
	if cfg.Version != GlobalRateLimitConfigVersion {
		logger.Warnf(
			"stored global rate limit config version %v does not match current version %v; skipping",
			cfg.Version,
			GlobalRateLimitConfigVersion,
		)
		return
	}
	fc.globalRateLimit = &cfg
}

func (fc *fetchConf) setGlobalSettings(cmd *redis.StringCmd, logger logrus.FieldLogger) {
	b, err := cmd.Bytes()
	if err != nil {
		if err != redis.Nil {
			logger.WithError(err).Warnf("error sending GET for key %v", redisGlobalSettingsConfigKey)
		}
		return
	}
	cfg := GlobalSettingsConfig{}
	if err := json.Unmarshal(b, &cfg); err != nil {
		logger.WithError(err).Warnf("error unmarshaling json for key %v", redisGlobalSettingsConfigKey)
		return
	}
	if cfg.Version != GlobalSettingsConfigVersion {
		logger.Warnf(
			"stored global settings config version %v does not match current version %v; skipping",
			cfg.Version,
			GlobalSettingsConfigVersion,
		)
		return
	}
	fc.globalSettings = &cfg
}

func (fc *fetchConf) setRateLimits(cmd *redis.StringStringMapCmd, logger logrus.FieldLogger) {
	rateLimits, err := cmd.Result()
	if err != nil {
		if err != redis.Nil {
			logger.WithError(err).Warnf("error sending GET for key %v", redisRateLimitsConfigKey)
		}
		return
	}
	for name, configString := range rateLimits {
		cfg := RateLimitConfig{}
		if err := json.Unmarshal([]byte(configString), &cfg); err != nil {
			logger.WithError(err).Warnf("error unmarshaling json for key %v value %v", redisRateLimitsConfigKey, name)
			continue
		}
		if cfg.Version != RateLimitConfigVersion {
			logger.Warnf(
				"stored rate limit config version %v does not match current version %v; skipping",
				cfg.Version,
				RateLimitConfigVersion,
			)
			continue
		}
		fc.rateLimitsByName[name] = cfg
		fc.rateLimitsByPath[cfg.Spec.Conditions.Path] = cfg
	}
}

func (fc *fetchConf) setJails(cmd *redis.StringStringMapCmd, logger logrus.FieldLogger) {
	jails, err := cmd.Result()
	if err != nil {
		if err != redis.Nil {
			logger.WithError(err).Warnf("error sending GET for key %v", redisJailsConfigKey)
		}
		return
	}
	for name, configString := range jails {
		cfg := JailConfig{}
		if err := json.Unmarshal([]byte(configString), &cfg); err != nil {
			logger.WithError(err).Warnf("error unmarshaling json for key %v value %v", redisJailsConfigKey, name)
			continue
		}
		if cfg.Version != JailConfigVersion {
			logger.Warnf(
				"stored jail config version %v does not match current version %v; skipping",
				cfg.Version,
				JailConfigVersion,
			)
			continue
		}
		fc.jailsByName[name] = cfg
		fc.jailsByPath[cfg.Spec.Conditions.Path] = cfg
	}
}

func (rs *RedisConfStore) fetchPrisoners() {
	lock, err := rs.obtainRedisPrisonersLock()

	if err != nil {
		rs.logger.Errorf("error obtaining lock in pipelined fetch: %v", err)
		return
	}

	defer rs.releaseRedisPrisonersLock(lock, time.Now().UTC())
	expiredPrisoners := []string{}
	cmd := rs.redis.HGetAll(redisPrisonersKey)
	// Note: In order to match the rest of the configuration, we purposely purge the prisoners regardless of whether we
	// can connect or update the data in Redis. This way, Guardian continues to "fail open"
	rs.conf.prisoners.purge()
	prisoners, err := cmd.Result()
	if err != nil {
		rs.logger.Errorf("error getting prisoners from redis: %v", err)
		return
	}
	for ip, prisonerJSON := range prisoners {
		var prisoner Prisoner
		err := json.Unmarshal([]byte(prisonerJSON), &prisoner)
		if err != nil {
			rs.logger.WithError(err).Warnf("unable to unmarshal json: %v", err)
			continue
		}
		if time.Now().UTC().Before(prisoner.Expiry) {
			rs.logger.Debugf("adding %v to prisoners\n", prisoner.IP.String())
			rs.conf.prisoners.addPrisonerFromStore(prisoner)
			continue
		}
		rs.logger.Debugf("removing %v from prisoners\n", prisoner.IP.String())
		expiredPrisoners = append(expiredPrisoners, ip)
	}

	if len(expiredPrisoners) == 0 {
		return
	}
	removeExpiredPrisonersCmd := rs.redis.HDel(redisPrisonersKey, expiredPrisoners...)
	n, err := removeExpiredPrisonersCmd.Result()
	if err != nil {
		rs.logger.Errorf("error removing expired prisoners: %v", err)
		return
	}
	rs.logger.Debugf("removed %d expired prisoners: %v", n, expiredPrisoners)
}

func (rs *RedisConfStore) pipelinedFetchConf() fetchConf {
	newConf := fetchConf{
		rateLimitsByName: make(map[string]RateLimitConfig),
		rateLimitsByPath: make(map[string]RateLimitConfig),
		jailsByName:      make(map[string]JailConfig),
		jailsByPath:      make(map[string]JailConfig),
	}

	rs.logger.Debugf("Sending GET for key %v", redisGlobalRateLimitConfigKey)
	rs.logger.Debugf("Sending GET for key %v", redisGlobalSettingsConfigKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisRateLimitsConfigKey)
	rs.logger.Debugf("Sending HGETALL for key %v", redisJailsConfigKey)
	rs.logger.Debugf("Sending HKEYS for key %v", redisIPWhitelistKey)
	rs.logger.Debugf("Sending HKEYS for key %v", redisIPBlacklistKey)

	pipe := rs.redis.Pipeline()
	defer pipe.Close()
	whitelistKeysCmd := pipe.HKeys(redisIPWhitelistKey)
	blacklistKeysCmd := pipe.HKeys(redisIPBlacklistKey)
	globalRateLimitCmd := pipe.Get(redisGlobalRateLimitConfigKey)
	globalSettingsCmd := pipe.Get(redisGlobalSettingsConfigKey)
	rateLimitsCmd := pipe.HGetAll(redisRateLimitsConfigKey)
	jailsCmd := pipe.HGetAll(redisJailsConfigKey)
	pipe.Exec()

	newConf.setWhitelist(whitelistKeysCmd, rs.logger)
	newConf.setBlacklist(blacklistKeysCmd, rs.logger)
	newConf.setGlobalRateLimit(globalRateLimitCmd, rs.logger)
	newConf.setGlobalSettings(globalSettingsCmd, rs.logger)
	newConf.setRateLimits(rateLimitsCmd, rs.logger)
	newConf.setJails(jailsCmd, rs.logger)
	rs.fetchPrisoners()
	return newConf
}
