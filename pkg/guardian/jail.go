package guardian

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"net"
	"net/url"
	"time"
)


// Jail is a namesake of a similar concept from fail2ban
// https://docs.plesk.com/en-US/obsidian/administrator-guide/73382/
// In the context of Guardian, a Jail is a combination of a Limit and a BanDuration.
// If the Limit is reached, the the IP will be banned for the BanDuration.
type Jail struct {
	Limit       Limit         `yaml:"limit""`
	BanDuration time.Duration `yaml:"ban_duration"`
}

// A Jailer can determine if the requests from a client has met the conditions of a Jail's Limit.
// If the limit has been exceeded, the Jailer should mark the ip as "jailed" for the entire BanDuration
type Jailer interface {
	IsJailed(ctx context.Context, request Request) (bool, error)
}

func CondStopOnJailed(jailer Jailer) CondRequestBlockerFunc {
	return func(ctx context.Context, req Request) (bool, bool, uint32, error) {
		jailed, err := jailer.IsJailed(ctx, req)
		if err != nil {
			return false, false, RequestsRemainingMax, errors.Wrap(err, "error checking if request is jailed")
		}
		if jailed {
			return true, true, 0, nil
		}
		return false, false, RequestsRemainingMax, nil
	}
}

type Warden interface {
	IsJailed(ip net.IP) bool
	AddJailed(ip net.IP, expiration time.Duration)
}


// A RouteJailStore can retrieve the Jail configuration for a given route
type RouteJailStore interface {
	GetJail(u url.URL) Jail
}

type JailProvider interface {
	GetJail(req Request) Jail
}

type RouteJailProvider struct {
	logger logrus.FieldLogger
	store RouteJailStore
}

func (jp *RouteJailProvider) GetJail(req Request) Jail {
	reqUrl, err := url.Parse(req.Path)
	if err != nil || reqUrl == nil {
		return Jail{} // Limit disabled by default
	}
	jp.logger.Debugf("Getting jail for %v", reqUrl)
	return jp.store.GetJail(*reqUrl)
}

// NewRouteRateLimitProvider returns an implementation of a LimitProvider intended to provide limits based off a
// a request's path and a client's IP address.
func NewRouteJailProvider(store RouteJailStore, logger logrus.FieldLogger) *RouteJailProvider {
	return &RouteJailProvider{store: store, logger: logger}
}

// routeJailKeyFunc provides a key unique to a particular client IP and request path.
func routeJailKeyFunc(req Request) string {
	return "jailer:" + req.RemoteAddress + ":" + req.Path
}

type GenericJailer struct {
	keyFunc func(req Request) string
	jailProvider JailProvider
	warden Warden
	counter Counter
	logger logrus.FieldLogger
}

func (gj *GenericJailer) IsJailed(ctx context.Context, request Request) (bool, error) {
	if gj.keyFunc == nil || gj.jailProvider == nil || gj.counter == nil || gj.logger == nil || gj.warden == nil {
		gj.logger.Error("misconfigured generic jailer: missing required field")
		return false, nil
	}

	ip := net.ParseIP(request.RemoteAddress)
	if gj.warden.IsJailed(ip) {
		return true, nil
	}

	jail := gj.jailProvider.GetJail(request)
	gj.logger.Debugf("fetched jail %v for request %v", jail, request.Path)
	if !jail.Limit.Enabled {
		gj.logger.Debugf("jail limit not enabled for %v, allowing", request)
		return false, nil
	}

	key := SlotKey(gj.keyFunc(request), time.Now().UTC(), jail.Limit.Duration)
	gj.logger.Debugf("generated key %v for request %v", key, request)

	currCount, blocked, err := gj.counter.Incr(ctx, key, 1, jail.Limit.Count, jail.Limit.Duration)
	if err != nil {
		err := errors.Wrap(err, fmt.Sprintf(" error incrementing counter for request: %v", request))
		gj.logger.WithError(err).Errorf("error incrementing counter")
		return false, err
	}

	banned := blocked || currCount > jail.Limit.Count
	if banned {
		gj.warden.AddJailed(ip, jail.BanDuration)
		gj.logger.Debugf("banning ip: %v, due to jail: %v", ip.String(), jail)
	}
	return false, nil
}

func NewGenericJailer(store RouteJailStore, logger logrus.FieldLogger, c Counter, w Warden) *GenericJailer {
	return &GenericJailer{
		keyFunc:      routeJailKeyFunc,
		jailProvider: NewRouteJailProvider(store, logger),
		warden:       w,
		counter:      c,
		logger:       logger,
	}
}