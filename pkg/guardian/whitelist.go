package guardian

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func IPNetsFromStrings(ipNetStrs []string, logger logrus.FieldLogger) []net.IPNet {
	ipNets := []net.IPNet{}
	for _, cidrString := range ipNetStrs {
		_, cidr, err := net.ParseCIDR(cidrString)
		if err != nil {
			logger.WithError(err).Errorf("error parsing cidr %v", cidrString)
			continue
		}

		ipNets = append(ipNets, *cidr)
	}

	return ipNets
}

func CondStopOnWhitelistFunc(whitelister *IPWhitelister) CondRequestBlockerFunc {
	f := func(context context.Context, req Request) (bool, bool, uint32, error) {
		whitelisted, err := whitelister.IsWhitelisted(context, req)
		if err != nil {
			return false, false, 0, errors.Wrap(err, "error checking if request is whitelisted")
		}

		if whitelisted {
			return true, false, RequestsRemainingMax, nil
		}

		return false, false, RequestsRemainingMax, nil
	}

	return f
}

type WhitelistProvider interface {
	GetWhitelist() []net.IPNet
}

func NewIPWhitelister(provider WhitelistProvider, logger logrus.FieldLogger, reporter MetricReporter) *IPWhitelister {
	return &IPWhitelister{provider: provider, logger: logger, reporter: reporter}
}

type IPWhitelister struct {
	provider WhitelistProvider
	logger   logrus.FieldLogger
	reporter MetricReporter
}

func (w *IPWhitelister) IsWhitelisted(context context.Context, req Request) (bool, error) {
	start := time.Now().UTC()
	whitelisted := false
	errorOccurred := false
	defer func() {
		w.reporter.HandledWhitelist(req, whitelisted, errorOccurred, time.Since(start))
	}()

	w.logger.Debugf("checking whitelist for request %#v", req)
	ip := net.ParseIP(req.RemoteAddress)
	w.logger.Debugf("parsed IP from request %#v", req)
	if ip == nil {
		errorOccurred = true
		return false, fmt.Errorf("invalid remote address -- not IP")
	}

	w.logger.Debug("Getting whitelist")
	whitelist := w.provider.GetWhitelist()
	w.logger.Debugf("Got whitelist with length %d", len(whitelist))
	w.reporter.CurrentWhitelist(whitelist)

	for _, cidr := range whitelist {
		if cidr.Contains(ip) {
			w.logger.Debugf("Found %v in cidr %v of whitelist", ip, cidr.String())
			whitelisted = true
			return true, nil
		}
		w.logger.Debugf("CIDR %v does not contain %v of whitelist", cidr.String(), ip)
	}

	w.logger.Debugf("%v NOT FOUND in whitelist", ip)
	return false, nil
}
