package identitymodule

import (
	"encoding/json"
	"fmt"
	"time"

	iiqidcache "github.com/Intent-IQ/identity-go/cache"
	iiqidaerospikestore "github.com/Intent-IQ/identity-go/integrations/aerospike"
	iiqidredisstore "github.com/Intent-IQ/identity-go/integrations/redis"
	iiqidvalkeystore "github.com/Intent-IQ/identity-go/integrations/valkey"
)

type Config struct {
	PartnerID             string `json:"partner_id" yaml:"partner_id"`
	APIEndpoint           string `json:"api_endpoint" yaml:"api_endpoint"`
	ReportsEndpoint       string `json:"reports_endpoint" yaml:"reports_endpoint"`
	Timeout               int64  `json:"timeout" yaml:"timeout"`
	WaitTimeout           *int64 `json:"wait_timeout" yaml:"wait_timeout"`
	MaxBackgroundS2SCalls int    `json:"max_background_s2s_calls" yaml:"max_background_s2s_calls"`

	Cache     iiqidcache.Config           `json:"cache" yaml:"cache"`
	Redis     *iiqidredisstore.Config     `json:"redis" yaml:"redis"`
	Valkey    *iiqidvalkeystore.Config    `json:"valkey" yaml:"valkey"`
	Aerospike *iiqidaerospikestore.Config `json:"aerospike" yaml:"aerospike"`

	MetricsEnabled bool `json:"metrics_enabled" yaml:"metrics_enabled"`
	MetricsPort    int  `json:"metrics_port" yaml:"metrics_port"`
}

func defaultConfig() Config {
	return Config{
		Timeout:        1000,
		MetricsEnabled: true,
		Cache: iiqidcache.Config{
			TTLSeconds:                  43_200,
			MaxKeys:                     10,
			MaxSize:                     100_000,
			TTLCeilingFirstPartySeconds: 86_400,
			TTLCeilingThirdPartySeconds: 43_200,
			TTLCeilingDeviceSeconds:     3_600,
			NegativeTTLSeconds:          120,
		},
	}
}

func (config Config) timeout() time.Duration {
	return time.Duration(config.Timeout) * time.Millisecond
}

func (config Config) waitTimeout() *time.Duration {
	if config.WaitTimeout == nil {
		return nil
	}
	wait := time.Duration(*config.WaitTimeout) * time.Millisecond
	return &wait
}

func (config Config) validate() error {
	if config.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if config.WaitTimeout != nil && *config.WaitTimeout < 0 {
		return fmt.Errorf("wait_timeout must not be negative")
	}
	if config.MaxBackgroundS2SCalls < 0 {
		return fmt.Errorf("max_background_s2s_calls must not be negative")
	}
	if config.WaitTimeout != nil && *config.WaitTimeout < config.Timeout && !config.Cache.Enabled {
		return fmt.Errorf("cache must be enabled when wait_timeout is less than timeout")
	}
	return nil
}

// resolve overlays account-level configuration on the module configuration.
func (config Config) resolve(accountConfig json.RawMessage) Config {
	if len(accountConfig) == 0 {
		return config
	}
	resolved := config
	if err := json.Unmarshal(accountConfig, &resolved); err != nil {
		return config
	}
	resolved.MaxBackgroundS2SCalls = config.MaxBackgroundS2SCalls
	if err := resolved.validate(); err != nil {
		return config
	}
	return resolved
}
