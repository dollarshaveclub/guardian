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

// CondStopOnJailed uses a jailer to determine if a request should be blocked or not. It will also stop further processing
// on the request if the client ip is jailed.
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

// PrisonerStore can check if a client ip is a prisoner and also add prisoners
type PrisonerStore interface {
	IsPrisoner(ip net.IP) bool
	AddPrisoner(ip net.IP, expiration time.Duration)
}

// A JailProvider is a generic interface for determining which jail (if any) applies to a request
type JailProvider interface {
	GetJail(req Request) Jail
}

// A RouteJailStore can retrieve the Jail configuration for a given route
type RouteJailStore interface {
	GetJail(u url.URL) Jail
}

// A RouteJailProvider is a jail provider that provides a jail based off the route of a request
type RouteJailProvider struct {
	logger logrus.FieldLogger
	store  RouteJailStore
}

// GetJail queries the store to determine which Jail, if any, applies to the request.
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
	keyFunc      func(req Request) string
	jailProvider JailProvider
	store        PrisonerStore
	counter      Counter
	logger       logrus.FieldLogger
	onJailHandled []JailedHandledHook
}

type JailedHandledHook func(req Request, jailed bool, dur time.Duration, err error)

func OnGenericJailerHandled(mr MetricReporter) JailedHandledHook {
	return func(req Request, jailed bool, dur time.Duration, err error) {
		opts := []MetricOptionSetter{}
		if jailed {
			// Only set the routes for requests that were jailed. This way, we know the cardinality of the custom metrics.
			opts = append(opts, WithRoute(req.Path))
		}
		mr.HandledJail(req, jailed, err != nil, dur, opts...)
	}
}

func (gj *GenericJailer) IsJailed(ctx context.Context, request Request) (jailed bool, err error) {
	if gj.keyFunc == nil || gj.jailProvider == nil || gj.counter == nil || gj.logger == nil || gj.store == nil {
		gj.logger.Error("misconfigured generic jailer: missing required field")
		return false, nil
	}

	start := time.Now().UTC()
	defer func() {
		for _, fn := range gj.onJailHandled {
			fn(request, jailed, time.Since(start), err)
		}
	}()

	ip := net.ParseIP(request.RemoteAddress)
	if gj.store.IsPrisoner(ip) {
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
		gj.store.AddPrisoner(ip, jail.BanDuration)
		gj.logger.Debugf("banning ip: %v, due to jail: %v", ip.String(), jail)
		return true, nil
	}

	return false, nil
}

func NewGenericJailer(store RouteJailStore, logger logrus.FieldLogger, c Counter, s PrisonerStore, mr MetricReporter) *GenericJailer {
	return &GenericJailer{
		keyFunc:      routeJailKeyFunc,
		jailProvider: NewRouteJailProvider(store, logger),
		store:        s,
		counter:      c,
		logger:       logger,
		// TODO: Expose this field for users to configure as needed
		onJailHandled: []JailedHandledHook{OnGenericJailerHandled(mr)},
	}
}
