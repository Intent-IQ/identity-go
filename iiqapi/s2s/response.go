package s2s

import (
	"encoding/json"
	"time"

	"github.com/prebid/openrtb/v20/openrtb2"
)

type Response struct {
	Data       json.RawMessage `json:"data"`
	CacheTTL   *int64          `json:"cttl"`
	ABTestUUID string          `json:"abTestUuid"`
	TC         *int64          `json:"tc"`
	Status     int             `json:"-"`
}

func (r Response) EIDs() []openrtb2.EID {
	var data struct {
		EIDs []openrtb2.EID `json:"eids"`
	}
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return nil
	}
	return data.EIDs
}

func (r Response) TTL() time.Duration {
	if r.CacheTTL == nil {
		return 0
	}
	return time.Duration(*r.CacheTTL) * time.Second
}
