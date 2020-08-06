package guardian

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Jail is a namesake of a similar concept from fail2ban
// https://docs.plesk.com/en-US/obsidian/administrator-guide/73382/
// In the context of Guardian, a Jail is a combination of a Limit and a BanDuration.
// If the Limit is reached, the the IP will be banned for the BanDuration.
type Jail struct {
	Limit       Limit         `yaml:"limit"" json:"limit"`
	BanDuration time.Duration `yaml:"banDuration" json:"banDuration"`
}

func (j Jail) String() string {
	return fmt.Sprintf("%v, BanDuration: %v", j.Limit, j.BanDuration)
}

// A Jailer can determine if the requests from a client has met the conditions of a Jail's Limit.
// If the limit has been exceeded, the Jailer should mark the ip as Banned for the entire BanDuration.
type Jailer interface {
	IsBanned(ctx context.Context, request Request) (bool, error)
}

// CondStopOnBanned uses a jailer to determine if a request should be blocked or not. It will also stop further processing
// on the request if the client ip is banned.
func CondStopOnBanned(jailer Jailer) CondRequestBlockerFunc {
	return func(ctx context.Context, req Request) (bool, bool, uint32, error) {
		banned, err := jailer.IsBanned(ctx, req)
		if err != nil {
			return false, false, RequestsRemainingMax, errors.Wrap(err, "error checking if request is banned")
		}
		if banned {
			return true, true, 0, nil
		}
		return false, false, RequestsRemainingMax, nil
	}
}

// PrisonerStore can check if a client ip is a prisoner and also add prisoners
type PrisonerStore interface {
	IsPrisoner(remoteAddress string) bool
	AddPrisoner(remoteAddress string, jail Jail)
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
	keyFunc         func(req Request) string
	jailProvider    JailProvider
	store           PrisonerStore
	counter         Counter
	logger          logrus.FieldLogger
	metricsReporter MetricReporter
}

func (gj *GenericJailer) IsBanned(ctx context.Context, request Request) (banned bool, err error) {
	if gj.keyFunc == nil || gj.jailProvider == nil || gj.counter == nil || gj.logger == nil || gj.store == nil {
		gj.logger.Error("misconfigured generic jailer: missing required field")
		return false, nil
	}

	start := time.Now().UTC()
	defer func() {
		opts := []MetricOptionSetter{}
		if banned {
			// Only set additional tags for requests that were blocked. This way, we know the cardinality of the custom metrics.
			opts = append(opts, WithRemoteAddress(request.RemoteAddress))
		}
		gj.metricsReporter.HandledJail(request, banned, err != nil, time.Since(start), opts...)
	}()

	banned = gj.store.IsPrisoner(request.RemoteAddress)
	if banned {
		return banned, nil
	}

	jail := gj.jailProvider.GetJail(request)
	gj.logger.Debugf("fetched jail %v for request %v", jail, request.Path)
	if !jail.Limit.Enabled {
		gj.logger.Debugf("jail limit not enabled for %v, allowing", request)
		return false, nil
	}

	currCount, err := gj.counter.Incr(ctx, gj.keyFunc(request), 1, jail.Limit)
	if err != nil {
		err := errors.Wrap(err, fmt.Sprintf(" error incrementing counter for request: %v", request))
		gj.logger.WithError(err).Errorf("error incrementing counter")
		return false, err
	}

	banned = currCount > jail.Limit.Count
	if banned {
		gj.store.AddPrisoner(request.RemoteAddress, jail)
		gj.logger.Debugf("banning ip: %v, due to jail: %v", request.RemoteAddress, jail)
	}

	return banned, nil
}

func NewGenericJailer(store RouteJailStore, logger logrus.FieldLogger, c Counter, s PrisonerStore, mr MetricReporter) *GenericJailer {
	return &GenericJailer{
		keyFunc:         routeJailKeyFunc,
		jailProvider:    NewRouteJailProvider(store, logger),
		store:           s,
		counter:         c,
		logger:          logger,
		metricsReporter: mr,
	}
}
