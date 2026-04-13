package alphaess

import "encoding/json"

// apiResponse is the envelope returned by all AlphaESS API endpoints.
// The Data field is left as raw JSON for per-endpoint unmarshaling.
type apiResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// PowerData represents the response from getLastPowerData.
type PowerData struct {
	Ppv   float64 `json:"ppv"`
	Pload float64 `json:"pload"`
	Pbat  float64 `json:"pbat"`
	Pgrid float64 `json:"pgrid"`
	Soc   float64 `json:"soc"`
}

// EnergyData represents the response from getOneDateEnergyBySn.
type EnergyData struct {
	Epv         float64 `json:"epv"`
	EInput      float64 `json:"eInput"`
	EOutput     float64 `json:"eOutput"`
	ECharge     float64 `json:"eCharge"`
	EDischarge  float64 `json:"eDischarge"`
	EGridCharge float64 `json:"eGridCharge"`
}

// PowerSnapshot represents a single 5-minute snapshot from getOneDayPowerBySn.
type PowerSnapshot struct {
	Cbat       float64 `json:"cbat"`
	Ppv        float64 `json:"ppv"`
	Load       float64 `json:"load"`
	FeedIn     float64 `json:"feedIn"`
	GridCharge float64 `json:"gridCharge"`
	UploadTime string  `json:"uploadTime"`
}

// SystemInfo represents a single system entry from getEssList.
type SystemInfo struct {
	SysSn     string  `json:"sysSn"`
	Cobat     float64 `json:"cobat"`
	Mbat      string  `json:"mbat"`
	Minv      string  `json:"minv"`
	Popv      float64 `json:"popv"`
	Poinv     float64 `json:"poinv"`
	EmsStatus string  `json:"emsStatus"`
}
