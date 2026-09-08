package aerospike

import (
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
)

const maxPort = 65535

type Config struct {
	Host      string       `json:"host"`
	Port      int          `json:"port"`
	Namespace string       `json:"namespace"`
	Set       string       `json:"set"`
	Policy    ClientPolicy `json:"client_policy"`
}

type ClientPolicy struct {
	ConnectionQueueSize   int `json:"connection_queue_size"`
	MinConnectionsPerNode int `json:"min_connections_per_node"`
	ConnectTimeoutMs      int `json:"connect_timeout_ms"`
	IdleTimeoutMs         int `json:"idle_timeout_ms"`
}

func (config Config) Validate() error {
	return validation.ValidateStruct(&config,
		validation.Field(&config.Host, validation.Required, is.Host),
		validation.Field(&config.Port, validation.Required, validation.Min(1), validation.Max(maxPort)),
		validation.Field(&config.Namespace, validation.Required),
		validation.Field(&config.Set, validation.Required),
		validation.Field(&config.Policy),
	)
}

func (policy ClientPolicy) Validate() error {
	return validation.ValidateStruct(&policy,
		validation.Field(&policy.ConnectionQueueSize, validation.Min(0)),
		validation.Field(&policy.MinConnectionsPerNode, validation.Min(0)),
		validation.Field(&policy.ConnectTimeoutMs, validation.Min(0)),
		validation.Field(&policy.IdleTimeoutMs, validation.Min(0)),
	)
}
