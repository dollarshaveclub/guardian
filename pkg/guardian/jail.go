package guardian

import "time"

type Jail struct {
	Limit       Limit         `yaml:"limit""`
	BanDuration time.Duration `yaml:"ban_duration"`
}