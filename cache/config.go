package cache

import "time"

type Config struct {
	Enabled                     bool   `json:"enabled"`
	Provider                    string `json:"provider"`
	TTLSeconds                  int    `json:"ttl_seconds"`
	MaxKeys                     int    `json:"max_keys"`
	MaxSize                     int    `json:"max_size"`
	TTLCeilingFirstPartySeconds int    `json:"ttl_ceiling_first_party_seconds"`
	TTLCeilingThirdPartySeconds int    `json:"ttl_ceiling_third_party_seconds"`
	TTLCeilingDeviceSeconds     int    `json:"ttl_ceiling_device_seconds"`
	NegativeTTLSeconds          int    `json:"negative_ttl_seconds"`
	InProgressTTLSeconds        int    `json:"in_progress_ttl_seconds"`
}

func (config Config) TTLPolicy() TTLPolicy {
	seconds := func(value int) time.Duration { return time.Duration(value) * time.Second }
	return TTLPolicy{
		Default:           seconds(config.TTLSeconds),
		FirstPartyCeiling: seconds(config.TTLCeilingFirstPartySeconds),
		ThirdPartyCeiling: seconds(config.TTLCeilingThirdPartySeconds),
		DeviceCeiling:     seconds(config.TTLCeilingDeviceSeconds),
		NegativeTTL:       seconds(config.NegativeTTLSeconds),
		InProgressTTL:     seconds(config.InProgressTTLSeconds),
	}
}
