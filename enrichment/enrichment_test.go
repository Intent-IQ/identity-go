package enrichment

import "testing"

func TestStableTokens(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "outcome enriched", got: string(OutcomeEnriched), want: "enriched"},
		{name: "outcome no ids", got: string(OutcomeNoIDs), want: "no_ids"},
		{name: "outcome cached no ids", got: string(OutcomeCachedNoIDs), want: "no_ids_cached"},
		{name: "outcome in progress", got: string(OutcomeInProgress), want: "in_progress"},
		{name: "outcome no endpoint", got: string(OutcomeNoEndpoint), want: "no_endpoint"},
		{name: "reason no ids", got: string(ReasonNoIDs), want: "no_ids"},
		{name: "reason cached no ids", got: string(ReasonNoIDsCached), want: "no_ids_cached"},
		{name: "reason in progress", got: string(ReasonInProgress), want: "in_progress"},
		{name: "reason no endpoint", got: string(ReasonNoEndpoint), want: "no_endpoint"},
		{name: "cache hit", got: string(CacheLookupHit), want: "hit"},
		{name: "cache miss", got: string(CacheLookupMiss), want: "miss"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("token = %q, want %q", tt.got, tt.want)
			}
		})
	}
}
