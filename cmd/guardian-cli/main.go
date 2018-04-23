package main

import (
	"fmt"
	"net"
	"os"

	"github.com/dollarshaveclub/guardian/pkg/guardian"
	"github.com/go-redis/redis"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

func main() {
	app := kingpin.New("guardian-cli", "cli interface for controlling guardian")
	logLevel := app.Flag("log-level", "log level.").Short('l').Default("error").OverrideDefaultFromEnvar("LOG_LEVEL").String()
	redisAddress := app.Flag("redis-address", "host:port.").Short('r').OverrideDefaultFromEnvar("REDIS_ADDRESS").Required().String()

	addWhitelistCmd := app.Command("add-whitelist", "Add CIDRs to the IP Whitelist")
	addCidrStrings := addWhitelistCmd.Arg("cidr", "CIDR").Required().Strings()

	removeWhitelistCmd := app.Command("remove-whitelist", "Remove CIDRs from the IP Whitelist")
	removeCidrStrings := removeWhitelistCmd.Arg("cidr", "CIDR").Required().Strings()

	listWhitelistCmd := app.Command("list-whitelist", "List whitelisted CIDRs")

	setLimitCmd := app.Command("set-limit", "Sets the IP rate limit")
	limitCount := setLimitCmd.Arg("count", "limit count").Required().Uint64()
	limitDuration := setLimitCmd.Arg("duration", "limit duration").Required().Duration()
	limitEnabled := setLimitCmd.Arg("enabled", "limit enabled").Required().Bool()

	getLimitCmd := app.Command("get-limit", "Gets the IP rate limit")

	setReportOnlyCmd := app.Command("set-report-only", "Sets the report only flag")
	reportOnly := setReportOnlyCmd.Arg("report-only", "report only enabled").Required().Bool()

	getReportOnlyCmd := app.Command("get-report-only", "Gets the report only flag")

	selectedCmd := kingpin.MustParse(app.Parse(os.Args[1:]))
	redisOpts := &redis.Options{Addr: *redisAddress}
	redis := redis.NewClient(redisOpts)
	logger := logrus.StandardLogger()
	redisConfStore := guardian.NewRedisConfStore(redis, guardian.Limit{}, false, logger)

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		level = logrus.WarnLevel
	}
	logger.SetLevel(level)

	switch selectedCmd {
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
	case listWhitelistCmd.FullCommand():
		whitelist, err := listWhitelist(redisConfStore, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error listing CIDRS: %v\n", err)
			os.Exit(1)
		}

		for _, cidr := range whitelist {
			fmt.Println(cidr.String())
		}
	case setLimitCmd.FullCommand():
		limit := guardian.Limit{Count: *limitCount, Duration: *limitDuration, Enabled: *limitEnabled}
		err := setLimit(redisConfStore, limit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error setting limit: %v\n", err)
			os.Exit(1)
		}
	case getLimitCmd.FullCommand():
		limit, err := getLimit(redisConfStore)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error getting limit: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%v\n", limit)
	case setReportOnlyCmd.FullCommand():
		err := setReportOnly(redisConfStore, *reportOnly)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error setting report only flag: %v\n", err)
			os.Exit(1)
		}
	case getReportOnlyCmd.FullCommand():
		reportOnly, err := getReportOnly(redisConfStore)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error getting report only flag: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(reportOnly)
	}

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

func listWhitelist(store *guardian.RedisConfStore, logger logrus.FieldLogger) ([]net.IPNet, error) {
	logger.Debugf("Fetching CIDRs from Redis")
	whitelist, err := store.FetchWhitelist()
	if err != nil {
		return nil, errors.Wrap(err, "error fetching whitelist")
	}
	logger.Debugf("Fetched CIDRs from Redis: %v", whitelist)

	return whitelist, nil
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

func setLimit(store *guardian.RedisConfStore, limit guardian.Limit) error {
	return store.SetLimit(limit)
}

func getLimit(store *guardian.RedisConfStore) (guardian.Limit, error) {
	return store.FetchLimit()
}

func setReportOnly(store *guardian.RedisConfStore, reportOnly bool) error {
	return store.SetReportOnly(reportOnly)
}

func getReportOnly(store *guardian.RedisConfStore) (bool, error) {
	return store.FetchReportOnly()
}
