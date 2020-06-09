package guardian

// ConfigKind identifies a kind of configuration resource
type ConfigKind string

// Config kinds
const (
	// GlobalRateLimitConfigKind identifies a Global Rate Limit config resource
	GlobalRateLimitConfigKind = "GlobalRateLimit"
	// RateLimitConfigKind identifies a Rate Limit config resource
	RateLimitConfigKind = "RateLimit"
	// JailConfigKind identifies a Jail config resource
	JailConfigKind = "Jail"
	// GlobalSettingsConfigKind identifies a Global Settings config resource
	GlobalSettingsConfigKind = "GlobalSettings"
)

// ConfigMetadata represents metadata associated with a configuration resource.
// Every configuration resource begins with this metadata.
type ConfigMetadata struct {
	// Version identifies the version of the configuration resource format
	Version string `yaml:"version" json:"version"`
	// Kind identifies the kind of the configuration resource
	Kind ConfigKind `yaml:"kind" json:"kind"`
	// Name uniquely identifies a configuration resource within the resource's Kind
	Name string `yaml:"name" json:"name"`
	// Description is a description to add context to a resource
	Description string `yaml:"description" json:"description"`
}

// Conditions represents conditions required for a Limit to be applied to
// a Request. Currently, Guardian only filters requests based on URL path,
// via RedisConfStore.GetRouteRateLimit(url.URL) or .GetJail(url.URL)
type Conditions struct {
	Path string `yaml:"path" json:"path"`
}

// GlobalRateLimitSpec represents the specification for a GlobalRateLimitConfig
type GlobalRateLimitSpec struct {
	Limit Limit `yaml:"limit" json:"limit"`
}

// GlobalSettingsSpec represents the specification for a GlobalSettingsConfig
type GlobalSettingsSpec struct {
	ReportOnly bool `yaml:"reportOnly" json:"reportOnly"`
}

// RateLimitSpec represents the specification for a RateLimitConfig
type RateLimitSpec struct {
	Limit      Limit      `yaml:"limit" json:"limit"`
	Conditions Conditions `yaml:"conditions" json:"conditions"`
}

// JailSpec represents the specification for a JailConfig
type JailSpec struct {
	Jail       `yaml:",inline" json:",inline"`
	Conditions Conditions `yaml:"conditions" json:"conditions"`
}

// GlobalRateLimitConfig represents a resource that configures the global rate limit
type GlobalRateLimitConfig struct {
	ConfigMetadata `yaml:",inline" json:",inline"`
	Spec           GlobalRateLimitSpec `yaml:"globalRateLimitSpec" json:"globalRateLimitSpec"`
}

// GlobalSettingsConfig represents a resource that configures global settings
type GlobalSettingsConfig struct {
	ConfigMetadata `yaml:",inline" json:",inline"`
	Spec           GlobalSettingsSpec `yaml:"globalSettingsSpec" json:"globalSettingsSpec"`
}

// RateLimitConfig represents a resource that configures a conditional rate limit
type RateLimitConfig struct {
	ConfigMetadata `yaml:",inline" json:",inline"`
	Spec           RateLimitSpec `yaml:"rateLimitSpec" json:"rateLimitSpec"`
}

// JailConfig represents a resource that configures a jail
type JailConfig struct {
	ConfigMetadata `yaml:",inline" json:",inline"`
	Spec           JailSpec `yaml:"jailSpec" json:"jailSpec"`
}

// Config represents a generic configuration file
type Config struct {
	ConfigMetadata      `yaml:",inline"`
	GlobalRateLimitSpec *GlobalRateLimitSpec `yaml:"globalRateLimitSpec"`
	GlobalSettingsSpec  *GlobalSettingsSpec  `yaml:"globalSettingsSpec"`
	RateLimitSpec       *RateLimitSpec       `yaml:"rateLimitSpec"`
	JailSpec            *JailSpec            `yaml:"jailSpec"`
}

// JailConfigEntryDeprecated represents an entry in the jail configuration format
// associated with the deprecated CLI
type JailConfigEntryDeprecated struct {
	Route string `yaml:"route"`
	Jail  Jail   `yaml:"jail"`
}

// JailConfigEntryDeprecated represents the jail configuration format associated
// with the deprecated CLI
type JailConfigDeprecated struct {
	Jails []JailConfigEntryDeprecated `yaml:"jails"`
}

// RouteRateLimitConfigEntryDeprecated represents an entry in the conditional
// rate limit configuration format associated with the deprecated CLI
type RouteRateLimitConfigEntryDeprecated struct {
	Route string `yaml:"route"`
	Limit Limit  `yaml:"limit"`
}

// RouteRateLimitConfigDeprecated represents the conditional rate limit
// configuration format associated with the deprecated CLI
type RouteRateLimitConfigDeprecated struct {
	RouteRateLimits []RouteRateLimitConfigEntryDeprecated `yaml:"route_rate_limits"`
}
