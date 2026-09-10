package cache

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestConfigJSONFields(t *testing.T) {
	want := map[string]any{
		"enabled":                         true,
		"provider":                        "redis",
		"ttl_seconds":                     float64(1),
		"max_keys":                        float64(2),
		"max_size":                        float64(3),
		"ttl_ceiling_first_party_seconds": float64(4),
		"ttl_ceiling_third_party_seconds": float64(5),
		"ttl_ceiling_device_seconds":      float64(6),
		"negative_ttl_seconds":            float64(7),
	}
	config := Config{
		Enabled:                     true,
		Provider:                    "redis",
		TTLSeconds:                  1,
		MaxKeys:                     2,
		MaxSize:                     3,
		TTLCeilingFirstPartySeconds: 4,
		TTLCeilingThirdPartySeconds: 5,
		TTLCeilingDeviceSeconds:     6,
		NegativeTTLSeconds:          7,
	}

	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON fields = %#v, want %#v", got, want)
	}
}
