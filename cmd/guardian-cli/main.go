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
	logLevel := app.Flag("log-level", "log level.").Short('l').Default("warn").OverrideDefaultFromEnvar("LOG_LEVEL").String()
	redisAddress := app.Flag("redis-address", "host:port.").Short('r').OverrideDefaultFromEnvar("REDIS_ADDRESS").Required().String()

	addWhitelistCmd := app.Command("add-whitelist", "Add CIDRs to the IP Whitelist")
	addCidrStrings := addWhitelistCmd.Arg("cidr", "CIDR").Required().Strings()

	removeWhitelistCmd := app.Command("remove-whitelist", "Remove CIDRs from the IP Whitelist")
	removeCidrStrings := removeWhitelistCmd.Arg("cidr", "CIDR").Required().Strings()

	listWhitelistCmd := app.Command("list-whitelist", "List whitelisted CIDRs")

	selectedCmd := kingpin.MustParse(app.Parse(os.Args[1:]))
	redisOpts := &redis.Options{Addr: *redisAddress}
	redis := redis.NewClient(redisOpts)

	logger := logrus.StandardLogger()
	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		level = logrus.WarnLevel
	}
	logger.SetLevel(level)

	switch selectedCmd {
	case addWhitelistCmd.FullCommand():
		err := addWhitelist(redis, *addCidrStrings, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error adding CIDRS: %v", err)
			os.Exit(1)
		}

	case removeWhitelistCmd.FullCommand():
		err := removeWhitelist(redis, *removeCidrStrings, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error removing CIDRS: %v", err)
			os.Exit(1)
		}
	case listWhitelistCmd.FullCommand():
		whitelist, err := listWhitelist(redis, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error listing CIDRS: %v", err)
			os.Exit(1)
		}

		for _, cidr := range whitelist {
			fmt.Println(cidr.String())
		}
	}

}

func addWhitelist(redis *redis.Client, cidrStrings []string, logger logrus.FieldLogger) error {
	redisWhitelistStore := guardian.NewRedisIPWhitelistStore(redis, logger)

	logger.Debugf("Converting CIDR strings: %v", cidrStrings)
	cidrs, err := convertCIDRStrings(cidrStrings)
	if err != nil {
		return errors.Wrap(err, "error parsing cidr")
	}
	logger.Debugf("Converted CIDR strings to CIDRs: %v", cidrs)

	logger.Debugf("Adding CIDRs to Redis")
	err = redisWhitelistStore.AddCidrs(cidrs)
	if err != nil {
		return errors.Wrap(err, "error adding cidrs to redis")
	}
	logger.Debugf("Added CIDRs to Redis")

	return nil
}

func removeWhitelist(redis *redis.Client, cidrStrings []string, logger logrus.FieldLogger) error {
	redisWhitelistStore := guardian.NewRedisIPWhitelistStore(redis, logger)

	logger.Debugf("Converting CIDR strings: %v", cidrStrings)
	cidrs, err := convertCIDRStrings(cidrStrings)
	if err != nil {
		return errors.Wrap(err, "error parsing cidr")
	}
	logger.Debugf("Converted CIDR strings to CIDRs: %v", cidrs)

	logger.Debugf("Removing CIDRs from Redis")
	err = redisWhitelistStore.RemoveCidrs(cidrs)
	if err != nil {
		return errors.Wrap(err, "error removing cidrs from redis")
	}
	logger.Debugf("Removed CIDRs from Redis")

	return nil
}

func listWhitelist(redis *redis.Client, logger logrus.FieldLogger) ([]net.IPNet, error) {
	redisWhitelistStore := guardian.NewRedisIPWhitelistStore(redis, logger)

	logger.Debugf("Fetching CIDRs from Redis")
	whitelist, err := redisWhitelistStore.FetchWhitelist()
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
