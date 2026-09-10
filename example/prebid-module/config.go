package identitymodule

import (
	"encoding/json"
	"time"

	iiqidcache "github.com/Intent-IQ/identity-go/cache"
	iiqidaerospikestore "github.com/Intent-IQ/identity-go/integrations/aerospike"
	iiqidredisstore "github.com/Intent-IQ/identity-go/integrations/redis"
	iiqidvalkeystore "github.com/Intent-IQ/identity-go/integrations/valkey"
)

type Config struct {
	PartnerID       string `json:"partner_id" yaml:"partner_id"`
	APIEndpoint     string `json:"api_endpoint" yaml:"api_endpoint"`
	ReportsEndpoint string `json:"reports_endpoint" yaml:"reports_endpoint"`
	Timeout         int64  `json:"timeout" yaml:"timeout"`

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

// resolve overlays account-level configuration on the module configuration.
func (config Config) resolve(accountConfig json.RawMessage) Config {
	if len(accountConfig) == 0 {
		return config
	}
	resolved := config
	if err := json.Unmarshal(accountConfig, &resolved); err != nil {
		return config
	}
	return resolved
}
