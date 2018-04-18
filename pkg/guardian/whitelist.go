package guardian

import (
	"context"
	"fmt"
	"net"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

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

type IPWhitelistStore interface {
	GetWhitelist() ([]net.IPNet, error)
}

func NewIPWhitelister(store IPWhitelistStore, logger logrus.FieldLogger) *IPWhitelister {
	return &IPWhitelister{store: store, logger: logger}
}

type IPWhitelister struct {
	store  IPWhitelistStore
	logger logrus.FieldLogger
}

func (w IPWhitelister) IsWhitelisted(context context.Context, req Request) (bool, error) {
	w.logger.Debugf("checking whitelist for request %#v", req)
	ip := net.ParseIP(req.RemoteAddress)
	w.logger.Debugf("parsed IP from request %#v", req)
	if ip == nil {
		return false, fmt.Errorf("invalid remote address -- not IP")
	}

	w.logger.Debug("Getting whitelist")
	whitelist, err := w.store.GetWhitelist()
	w.logger.Debugf("Got whitelist with length %d", len(whitelist))

	if err != nil {
		return false, errors.Wrap(err, "error fetching whitelist")
	}

	for _, cidr := range whitelist {
		if cidr.Contains(ip) {
			w.logger.Debugf("Found %v in cidr %v of whitelist", ip, cidr.String())
			return true, nil
		}
		w.logger.Debugf("CIDR %v does not contain %v of whitelist", cidr.String(), ip)
	}

	w.logger.Debugf("%v NOT FOUND in whitelist", ip)
	return false, nil
}
