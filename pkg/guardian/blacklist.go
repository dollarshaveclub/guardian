package guardian

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func CondStopOnBlacklistFunc(blacklister *IPBlacklister) CondRequestBlockerFunc {
	f := func(context context.Context, req Request) (bool, bool, uint32, error) {
		blacklisted, err := blacklister.IsBlacklisted(context, req)
		if err != nil {
			return false, false, 0, errors.Wrap(err, "error checking if request is blacklisted")
		}

		if blacklisted {
			return true, true, RequestsRemainingMax, nil
		}

		return false, false, RequestsRemainingMax, nil
	}

	return f
}

type BlacklistProvider interface {
	GetBlacklist() []net.IPNet
}

func NewIPBlacklister(provider BlacklistProvider, logger logrus.FieldLogger, reporter MetricReporter) *IPBlacklister {
	return &IPBlacklister{provider: provider, logger: logger, reporter: reporter}
}

type IPBlacklister struct {
	provider BlacklistProvider
	logger   logrus.FieldLogger
	reporter MetricReporter
}

func (w *IPBlacklister) IsBlacklisted(context context.Context, req Request) (bool, error) {
	start := time.Now().UTC()
	blacklisted := false
	errorOccurred := false
	defer func() {
		w.reporter.HandledBlacklist(req, blacklisted, errorOccurred, time.Since(start))
	}()

	w.logger.Debugf("checking blacklist for request %#v", req)
	ip := net.ParseIP(req.RemoteAddress)
	w.logger.Debugf("parsed IP from request %#v", req)
	if ip == nil {
		errorOccurred = true
		return false, fmt.Errorf("invalid remote address -- not IP")
	}

	w.logger.Debug("Getting blacklist")
	blacklist := w.provider.GetBlacklist()
	w.logger.Debugf("Got blacklist with length %d", len(blacklist))

	for _, cidr := range blacklist {
		if cidr.Contains(ip) {
			w.logger.Debugf("Found %v in cidr %v of blacklist", ip, cidr.String())
			blacklisted = true
			return true, nil
		}
	}

	w.logger.Debugf("%v NOT FOUND in blacklist", ip)
	return false, nil
}
