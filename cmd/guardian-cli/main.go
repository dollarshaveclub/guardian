package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/dollarshaveclub/guardian/pkg/guardian"
	"github.com/go-redis/redis"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
	yaml "gopkg.in/yaml.v2"
)

func main() {
	app := kingpin.New("guardian-cli", "cli interface for controlling guardian")
	logLevel := app.Flag("log-level", "log level.").Short('l').Default("error").OverrideDefaultFromEnvar("LOG_LEVEL").String()
	redisAddress := app.Flag("redis-address", "host:port.").Short('r').OverrideDefaultFromEnvar("REDIS_ADDRESS").Required().String()

	// Configuration
	applyCmd := app.Command("apply", "Apply configuration resources from a YAML file")
	applyConfigFilePaths := applyCmd.Arg("config-file", "Path to configuration file").Required().Strings()

	// Getting configuration data
	getCmd := app.Command("get", "Get configuration resources of a certain kind")
	getConfigKind := getCmd.Arg("kind", "kind of resource").Required().String()

	// Removing configuration data
	deleteCmd := app.Command("delete", "Delete configuration resources")
	deleteConfigKind := deleteCmd.Arg("kind", "kind of resource").Required().String()
	deleteConfigName := deleteCmd.Arg("name", "name of resource").Required().String()

	// Whitelisting
	addWhitelistCmd := app.Command("add-whitelist", "Add CIDRs to the IP Whitelist")
	addCidrStrings := addWhitelistCmd.Arg("cidr", "CIDR").Required().Strings()

	removeWhitelistCmd := app.Command("remove-whitelist", "Remove CIDRs from the IP Whitelist")
	removeCidrStrings := removeWhitelistCmd.Arg("cidr", "CIDR").Required().Strings()

	getWhitelistCmd := app.Command("get-whitelist", "Get whitelisted CIDRs")

	// Blacklisting
	addBlacklistCmd := app.Command("add-blacklist", "Add CIDRs to the IP Blacklist")
	addBlacklistCidrStrings := addBlacklistCmd.Arg("cidr", "CIDR").Required().Strings()

	removeBlacklistCmd := app.Command("remove-blacklist", "Remove CIDRs from the IP Blacklist")
	removeBlacklistCidrStrings := removeBlacklistCmd.Arg("cidr", "CIDR").Required().Strings()

	getBlacklistCmd := app.Command("get-blacklist", "Get blacklisted CIDRs")

	// Rate limiting (deprecated CLI)
	setLimitCmd := app.Command("set-limit", "Sets the IP rate limit")
	limitCount := setLimitCmd.Arg("count", "limit count").Required().Uint64()
	limitDuration := setLimitCmd.Arg("duration", "limit duration").Required().Duration()
	limitEnabled := setLimitCmd.Arg("enabled", "limit enabled").Required().Bool()

	getLimitCmd := app.Command("get-limit", "Gets the IP rate limit")

	// Route rate limiting (deprecated CLI)
	setRouteRateLimitsCmd := app.Command("set-route-rate-limits", "Sets rate limits for provided routes")
	configFilePath := setRouteRateLimitsCmd.Arg("route-rate-limit-config-file", "path to configuration file").Required().String()
	removeRouteRateLimitsCmd := app.Command("remove-route-rate-limits", "Removes rate limits for provided routes")
	removeRouteRateLimitStrings := removeRouteRateLimitsCmd.Arg("routes", "Comma seperated list of routes to remove").Required().String()
	getRouteRateLimitsCmd := app.Command("get-route-rate-limits", "Gets the IP rate limits for each route")

	// Jails (deprecated CLI)
	setJailsCmd := app.Command("set-jails", "Sets rate limits for provided routes")
	jailsConfigFilePath := setJailsCmd.Arg("jail-config-file", "Path to configuration file").Required().String()
	removeJailsCmd := app.Command("remove-jails", "Removes rate limits for provided routes")
	removeJailsArgs := removeJailsCmd.Arg("jail-routes", "Comma separated list of jails to remove. Use the name of the route").Required().String()
	getJailsCmd := app.Command("get-jails", "Lists all of the jails")
	getPrisonersCmd := app.Command("get-prisoners", "List all prisoners")
	removePrisonersCmd := app.Command("remove-prisoners", "Removes prisoners from")
	prisoners := removePrisonersCmd.Arg("prisoners", "Comma separated list of ip address to remove").Required().String()

	// Report Only (deprecated CLI)
	setReportOnlyCmd := app.Command("set-report-only", "Sets the report only flag")
	reportOnly := setReportOnlyCmd.Arg("report-only", "report only enabled").Required().Bool()

	getReportOnlyCmd := app.Command("get-report-only", "Gets the report only flag")

	selectedCmd := kingpin.MustParse(app.Parse(os.Args[1:]))
	redisOpts := &redis.Options{Addr: *redisAddress}
	redis := redis.NewClient(redisOpts)
	logger := logrus.StandardLogger()
	redisConfStore, err := guardian.NewRedisConfStore(redis, []net.IPNet{}, []net.IPNet{}, guardian.Limit{}, false, false, 1000, logger, nil)
	if err != nil {
		fatalerror(fmt.Errorf("unable to create RedisConfStore: %v", err))
	}

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		level = logrus.WarnLevel
	}
	logger.SetLevel(level)

	fmt.Println(redisConfStore.GetLimit())

	switch selectedCmd {
	case applyCmd.FullCommand():
		err := applyConfig(redisConfStore, *applyConfigFilePaths, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error applying configuration: %v\n", err)
			os.Exit(1)
		}
	case getCmd.FullCommand():
		err := getConfig(redisConfStore, *getConfigKind, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error getting configuration: %v\n", err)
			os.Exit(1)
		}
	case deleteCmd.FullCommand():
		err := deleteConfig(redisConfStore, *deleteConfigKind, *deleteConfigName, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error deleting configuration: %v\n", err)
			os.Exit(1)
		}
	case addWhitelistCmd.FullCommand():
		err := addWhitelist(redisConfStore, *addCidrStrings, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error adding CIDRS: %v\n", err)
			os.Exit(1)
		}

	case removeWhitelistCmd.FullCommand():
		err := removeWhitelist(redisConfStore, *removeCidrStrings, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error removing CIDRS: %v\n", err)
			os.Exit(1)
		}
	case getWhitelistCmd.FullCommand():
		whitelist, err := getWhitelist(redisConfStore, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error listing CIDRS: %v\n", err)
			os.Exit(1)
		}

		for _, cidr := range whitelist {
			fmt.Println(cidr.String())
		}
	case addBlacklistCmd.FullCommand():
		err := addBlacklist(redisConfStore, *addBlacklistCidrStrings, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error adding CIDRS: %v\n", err)
			os.Exit(1)
		}

	case removeBlacklistCmd.FullCommand():
		err := removeBlacklist(redisConfStore, *removeBlacklistCidrStrings, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error removing CIDRS: %v\n", err)
			os.Exit(1)
		}
	case getBlacklistCmd.FullCommand():
		blacklist, err := getBlacklist(redisConfStore, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error listing CIDRS: %v\n", err)
			os.Exit(1)
		}

		for _, cidr := range blacklist {
			fmt.Println(cidr.String())
		}
	case setLimitCmd.FullCommand():
		fmt.Fprintf(os.Stderr, "%s is deprecated: apply a GlobalRateLimit config instead\n", setLimitCmd.FullCommand())
		limit := guardian.Limit{Count: *limitCount, Duration: *limitDuration, Enabled: *limitEnabled}
		err := setLimit(redisConfStore, limit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error setting limit: %v\n", err)
			os.Exit(1)
		}
	case getLimitCmd.FullCommand():
		fmt.Fprintf(os.Stderr, "%s is deprecated: get GlobalRateLimit instead\n", getLimitCmd.FullCommand())
		limit, err := getLimit(redisConfStore)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error getting limit: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%v\n", limit)
	case setRouteRateLimitsCmd.FullCommand():
		fmt.Fprintf(os.Stderr, "%s is deprecated: apply a RateLimit config instead\n", setRouteRateLimitsCmd.FullCommand())
		err := setRouteRateLimits(redisConfStore, *configFilePath)
		if err != nil {
			fatalerror(fmt.Errorf("error setting route rate limits: %v", err))
		}
	case getRouteRateLimitsCmd.FullCommand():
		fmt.Fprintf(os.Stderr, "%s is deprecated: get RateLimit instead\n", getRouteRateLimitsCmd.FullCommand())
		routeRateLimits, err := getRouteRateLimits(redisConfStore)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error getting route rate limits: %v\n", err)
			os.Exit(1)
		}
		config := guardian.RouteRateLimitConfigDeprecated{}
		for url, limit := range routeRateLimits {
			entry := guardian.RouteRateLimitConfigEntryDeprecated{
				Route: url.EscapedPath(),
				Limit: limit,
			}
			config.RouteRateLimits = append(config.RouteRateLimits, entry)
		}
		configYaml, err := yaml.Marshal(config)
		if err != nil {
			fatalerror(fmt.Errorf("error marshaling route limit yaml: %v", err))
		}
		fmt.Println(string(configYaml))
	case removeRouteRateLimitsCmd.FullCommand():
		err := removeRouteRateLimits(redisConfStore, *removeRouteRateLimitStrings)
		if err != nil {
			fatalerror(fmt.Errorf("error remove route rate limits: %v", err))
		}
	case setReportOnlyCmd.FullCommand():
		fmt.Fprintf(os.Stderr, "%s is deprecated: apply a GlobalSettings config instead\n", setReportOnlyCmd.FullCommand())
		err := setReportOnly(redisConfStore, *reportOnly)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error setting report only flag: %v\n", err)
			os.Exit(1)
		}
	case getReportOnlyCmd.FullCommand():
		fmt.Fprintf(os.Stderr, "%s is deprecated: get GlobalSettings instead\n", setReportOnlyCmd.FullCommand())
		reportOnly, err := getReportOnly(redisConfStore)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error getting report only flag: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(reportOnly)
	case setJailsCmd.FullCommand():
		fmt.Fprintf(os.Stderr, "%s is deprecated: apply a Jail config instead\n", setJailsCmd.FullCommand())
		err := setJails(redisConfStore, *jailsConfigFilePath)
		if err != nil {
			fatalerror(fmt.Errorf("error setting jails: %v", err))
		}
	case removeJailsCmd.FullCommand():
		err := removeJails(redisConfStore, *removeJailsArgs)
		if err != nil {
			fatalerror(err)
		}
	case getJailsCmd.FullCommand():
		fmt.Fprintf(os.Stderr, "%s is deprecated: get Jail instead", getJailsCmd.FullCommand())
		jails, err := getJails(redisConfStore)
		config := guardian.JailConfigDeprecated{}
		for u, j := range jails {
			entry := guardian.JailConfigEntryDeprecated{
				Route: u.EscapedPath(),
				Jail:  j,
			}
			config.Jails = append(config.Jails, entry)
		}
		configYaml, err := yaml.Marshal(config)
		if err != nil {
			fatalerror(fmt.Errorf("error marshaling jails yaml: %v", err))
		}
		fmt.Println(string(configYaml))
	case removePrisonersCmd.FullCommand():
		n, err := removePrisoners(redisConfStore, *prisoners)
		if err != nil {
			fatalerror(fmt.Errorf("error removing prisoners: %v", err))
		}
		fmt.Printf("removed %d prisoners\n", n)
	case getPrisonersCmd.FullCommand():
		prisoners, err := getPrisoners(redisConfStore)
		if err != nil {
			fatalerror(fmt.Errorf("error fetching prisoners: %v", err))
		}
		prisonersJson, err := yaml.Marshal(prisoners)
		if err != nil {
			fatalerror(fmt.Errorf("error marshaling prisoners: %v", err))
		}
		fmt.Println(string(prisonersJson))
	}
}

func applyConfig(store *guardian.RedisConfStore, configFilePaths []string, logger logrus.FieldLogger) error {
	for _, configFilePath := range configFilePaths {
		file, err := os.Open(configFilePath)
		if err != nil {
			return fmt.Errorf("error opening config file: %v", err)
		}
		defer file.Close()
		dec := yaml.NewDecoder(file)
		for {
			var config struct {
				guardian.ConfigMetadata `yaml:",inline"`
				Spec                    interface{} `yaml:"spec"`
			}
			err := dec.Decode(&config)
			if err == io.EOF { // No more YAML documents to read
				break
			} else if err != nil {
				return fmt.Errorf("error decoding yaml: %v", err)
			}
			configYaml, err := yaml.Marshal(&config)
			if err != nil {
				return fmt.Errorf("error marshaling yaml: %v", err)
			}
			switch config.Kind {
			case guardian.GlobalRateLimitConfigKind:
				config := guardian.GlobalRateLimitConfig{}
				if err := yaml.Unmarshal(configYaml, &config); err != nil {
					return fmt.Errorf("error unmarshaling yaml: %v", err)
				}
				if err := applyGlobalRateLimitConfig(store, config); err != nil {
					return err
				}
			case guardian.RateLimitConfigKind:
				config := guardian.RateLimitConfig{}
				if err := yaml.Unmarshal(configYaml, &config); err != nil {
					return fmt.Errorf("error unmarshaling yaml: %v", err)
				}
				if err := applyRateLimitConfig(store, config); err != nil {
					return err
				}
			case guardian.JailConfigKind:
				config := guardian.JailConfig{}
				if err := yaml.Unmarshal(configYaml, &config); err != nil {
					return fmt.Errorf("error unmarshaling yaml: %v", err)
				}
				if err := applyJailConfig(store, config); err != nil {
					return err
				}
			case guardian.GlobalSettingsConfigKind:
				config := guardian.GlobalSettingsConfig{}
				err = yaml.Unmarshal(configYaml, &config)
				if err != nil {
					return fmt.Errorf("error unmarshaling yaml: %v", err)
				}
				if err := applyGlobalSettingsConfig(store, config); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unrecognized config file kind: %v", config.Kind)
			}
		}
	}
	return nil
}

func getConfig(store *guardian.RedisConfStore, configKind string, logger logrus.FieldLogger) error {
	switch configKind {
	case guardian.GlobalRateLimitConfigKind:
		config, err := store.FetchGlobalRateLimitConfig()
		if err != nil {
			return fmt.Errorf("error getting global rate limit config: %v", err)
		}
		configYaml, err := yaml.Marshal(config)
		if err != nil {
			return fmt.Errorf("error marshaling yaml: %v", err)
		}
		fmt.Println(string(configYaml))
	case guardian.GlobalSettingsConfigKind:
		config, err := store.FetchGlobalSettingsConfig()
		if err != nil {
			return fmt.Errorf("error getting global settings config: %v", err)
		}
		configYaml, err := yaml.Marshal(config)
		if err != nil {
			return fmt.Errorf("error marshaling yaml: %v", err)
		}
		fmt.Println(string(configYaml))
	case guardian.RateLimitConfigKind:
		configs := store.FetchRateLimitConfigs()
		for _, config := range configs {
			configYaml, err := yaml.Marshal(config)
			if err != nil {
				return fmt.Errorf("error marshaling yaml: %v", err)
			}
			fmt.Println(string(configYaml))
			fmt.Println("---")
		}
	case guardian.JailConfigKind:
		configs := store.FetchJailConfigs()
		for _, config := range configs {
			configYaml, err := yaml.Marshal(config)
			if err != nil {
				return fmt.Errorf("error marshaling yaml: %v", err)
			}
			fmt.Println(string(configYaml))
			fmt.Println("---")
		}
	}
	return nil
}

func deleteConfig(store *guardian.RedisConfStore, configKind string, configName string, logger logrus.FieldLogger) error {
	switch configKind {
	case guardian.RateLimitConfigKind:
		err := store.DeleteRateLimitConfig(configName)
		if err != nil {
			return fmt.Errorf("error deleting rate limit config: %v", err)
		}
	case guardian.JailConfigKind:
		err := store.DeleteJailConfig(configName)
		if err != nil {
			return fmt.Errorf("error deleting jail config: %v", err)
		}
	default:
		return fmt.Errorf("config kind %v does not support deletion", configKind)
	}
	return nil
}

func addWhitelist(store *guardian.RedisConfStore, cidrStrings []string, logger logrus.FieldLogger) error {
	logger.Debugf("Converting CIDR strings: %v", cidrStrings)
	cidrs, err := convertCIDRStrings(cidrStrings)
	if err != nil {
		return errors.Wrap(err, "error parsing cidr")
	}
	logger.Debugf("Converted CIDR strings to CIDRs: %v", cidrs)

	logger.Debugf("Adding CIDRs to Redis")
	err = store.AddWhitelistCidrs(cidrs)
	if err != nil {
		return errors.Wrap(err, "error adding cidrs to redis")
	}
	logger.Debugf("Added CIDRs to Redis")

	return nil
}

func removeWhitelist(store *guardian.RedisConfStore, cidrStrings []string, logger logrus.FieldLogger) error {
	logger.Debugf("Converting CIDR strings: %v", cidrStrings)
	cidrs, err := convertCIDRStrings(cidrStrings)
	if err != nil {
		return errors.Wrap(err, "error parsing cidr")
	}
	logger.Debugf("Converted CIDR strings to CIDRs: %v", cidrs)

	logger.Debugf("Removing CIDRs from Redis")
	err = store.RemoveWhitelistCidrs(cidrs)
	if err != nil {
		return errors.Wrap(err, "error removing cidrs from redis")
	}
	logger.Debugf("Removed CIDRs from Redis")

	return nil
}

func getWhitelist(store *guardian.RedisConfStore, logger logrus.FieldLogger) ([]net.IPNet, error) {
	logger.Debugf("Fetching CIDRs from Redis")
	whitelist, err := store.FetchWhitelist()
	if err != nil {
		return nil, errors.Wrap(err, "error fetching whitelist")
	}
	logger.Debugf("Fetched CIDRs from Redis: %v", whitelist)

	return whitelist, nil
}

func addBlacklist(store *guardian.RedisConfStore, cidrStrings []string, logger logrus.FieldLogger) error {
	logger.Debugf("Converting CIDR strings: %v", cidrStrings)
	cidrs, err := convertCIDRStrings(cidrStrings)
	if err != nil {
		return errors.Wrap(err, "error parsing cidr")
	}
	logger.Debugf("Converted CIDR strings to CIDRs: %v", cidrs)

	logger.Debugf("Adding CIDRs to Redis")
	err = store.AddBlacklistCidrs(cidrs)
	if err != nil {
		return errors.Wrap(err, "error adding cidrs to redis")
	}
	logger.Debugf("Added CIDRs to Redis")

	return nil
}

func removeBlacklist(store *guardian.RedisConfStore, cidrStrings []string, logger logrus.FieldLogger) error {
	logger.Debugf("Converting CIDR strings: %v", cidrStrings)
	cidrs, err := convertCIDRStrings(cidrStrings)
	if err != nil {
		return errors.Wrap(err, "error parsing cidr")
	}
	logger.Debugf("Converted CIDR strings to CIDRs: %v", cidrs)

	logger.Debugf("Removing CIDRs from Redis")
	err = store.RemoveBlacklistCidrs(cidrs)
	if err != nil {
		return errors.Wrap(err, "error removing cidrs from redis")
	}
	logger.Debugf("Removed CIDRs from Redis")

	return nil
}

func getBlacklist(store *guardian.RedisConfStore, logger logrus.FieldLogger) ([]net.IPNet, error) {
	logger.Debugf("Fetching CIDRs from Redis")
	blacklist, err := store.FetchBlacklist()
	if err != nil {
		return nil, errors.Wrap(err, "error fetching blacklist")
	}
	logger.Debugf("Fetched CIDRs from Redis: %v", blacklist)

	return blacklist, nil
}

func convertCIDRStrings(cidrStrings []string) ([]net.IPNet, error) {
	cidrs := []net.IPNet{}
	for _, cidrString := range cidrStrings {
		_, cidr, err := net.ParseCIDR(cidrString)

		if err != nil {
			return nil, err
		}
		cidrs = append(cidrs, *cidr)
	}

	return cidrs, nil
}

func applyGlobalRateLimitConfig(store *guardian.RedisConfStore, config guardian.GlobalRateLimitConfig) error {
	return store.ApplyGlobalRateLimitConfig(config)
}

func applyGlobalSettingsConfig(store *guardian.RedisConfStore, config guardian.GlobalSettingsConfig) error {
	return store.ApplyGlobalSettingsConfig(config)
}

func applyRateLimitConfig(store *guardian.RedisConfStore, config guardian.RateLimitConfig) error {
	return store.ApplyRateLimitConfig(config)
}

func applyJailConfig(store *guardian.RedisConfStore, config guardian.JailConfig) error {
	return store.ApplyJailConfig(config)
}

func setLimit(store *guardian.RedisConfStore, limit guardian.Limit) error {
	return store.SetLimitDeprecated(limit)
}

func getLimit(store *guardian.RedisConfStore) (guardian.Limit, error) {
	return store.FetchLimitDeprecated()
}

func setReportOnly(store *guardian.RedisConfStore, reportOnly bool) error {
	return store.SetReportOnlyDeprecated(reportOnly)
}

func getReportOnly(store *guardian.RedisConfStore) (bool, error) {
	return store.FetchReportOnlyDeprecated()
}

func getRouteRateLimits(store *guardian.RedisConfStore) (map[url.URL]guardian.Limit, error) {
	return store.FetchRouteRateLimitsDeprecated()
}

func removeRouteRateLimits(store *guardian.RedisConfStore, routes string) error {
	var urls []url.URL
	for _, route := range strings.Split(routes, ",") {
		unwantedURL, err := url.Parse(route)
		if err != nil {
			return fmt.Errorf("error parsing route: %v", err)
		}
		urls = append(urls, *unwantedURL)
	}
	return store.RemoveRouteRateLimitsDeprecated(urls)
}

func setRouteRateLimits(store *guardian.RedisConfStore, configFilePath string) error {
	routeRateLimits := make(map[url.URL]guardian.Limit)
	content, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return fmt.Errorf("error reading config file: %v", err)
	}
	config := guardian.RouteRateLimitConfigDeprecated{}
	err = yaml.Unmarshal(content, &config)
	if err != nil {
		return fmt.Errorf("error unmarshaling yaml: %v", err)
	}
	for _, routeRateLimitEntry := range config.RouteRateLimits {
		configuredURL, err := url.Parse(routeRateLimitEntry.Route)
		if err != nil {
			return fmt.Errorf("error parsing route: %v", err)
		}
		routeRateLimits[*configuredURL] = routeRateLimitEntry.Limit
	}
	return store.SetRouteRateLimitsDeprecated(routeRateLimits)
}

func setJails(store *guardian.RedisConfStore, configFilePath string) error {
	jails := make(map[url.URL]guardian.Jail)
	content, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return fmt.Errorf("error reading config file: %v", err)
	}
	config := guardian.JailConfigDeprecated{}
	err = yaml.Unmarshal(content, &config)
	if err != nil {
		return fmt.Errorf("error unmarshaling yaml: %v", err)
	}
	for _, jailEntry := range config.Jails {
		configuredURL, err := url.Parse(jailEntry.Route)
		if err != nil {
			return fmt.Errorf("error parsing route: %v", err)
		}
		jails[*configuredURL] = jailEntry.Jail
	}
	return store.SetJailsDeprecated(jails)
}

func removeJails(store *guardian.RedisConfStore, routes string) error {
	var urls []url.URL
	for _, route := range strings.Split(routes, ",") {
		unwantedURL, err := url.Parse(route)
		if err != nil {
			return fmt.Errorf("error parsing route: %v", err)
		}
		urls = append(urls, *unwantedURL)
	}
	return store.RemoveJailsDeprecated(urls)
}

func getJails(store *guardian.RedisConfStore) (map[url.URL]guardian.Jail, error) {
	return store.FetchJailsDeprecated()
}

func getPrisoners(store *guardian.RedisConfStore) ([]guardian.Prisoner, error) {
	return store.FetchPrisoners()
}

func removePrisoners(store *guardian.RedisConfStore, prisoners string) (int64, error) {
	input := []net.IP{}
	for _, p := range strings.Split(prisoners, ",") {
		input = append(input, net.ParseIP(p))
	}
	return store.RemovePrisoners(input)
}

func fatalerror(err error) {
	fmt.Fprintf(os.Stderr, err.Error())
	os.Exit(1)
}
