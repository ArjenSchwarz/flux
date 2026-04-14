package alphaess

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client is an HTTP client for the AlphaESS Open API.
type Client struct {
	baseURL    string
	appID      string
	appSecret  string
	httpClient *http.Client
}

// NewClient creates a new AlphaESS API client.
func NewClient(appID, appSecret string, timeout time.Duration) *Client {
	return &Client{
		baseURL:    "https://openapi.alphaess.com/api",
		appID:      appID,
		appSecret:  appSecret,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// sign generates the timestamp and SHA-512 signature for API authentication.
func (c *Client) sign() (timestamp string, signature string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	h := sha512.New()
	h.Write([]byte(c.appID + c.appSecret + ts))
	return ts, hex.EncodeToString(h.Sum(nil))
}

// doGet sends an authenticated GET to the given endpoint with query parameters
// and unmarshals the API envelope. It returns the raw Data field for per-endpoint unmarshaling.
func (c *Client) doGet(ctx context.Context, endpoint string, params map[string]string) (json.RawMessage, error) {
	u := c.baseURL + "/" + endpoint
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		u += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", endpoint, err)
	}

	ts, sig := c.sign()
	req.Header.Set("appId", c.appID)
	req.Header.Set("timeStamp", ts)
	req.Header.Set("sign", sig)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", endpoint, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", endpoint, resp.StatusCode)
	}

	var envelope apiResponse
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("%s: unmarshal response: %w", endpoint, err)
	}

	if envelope.Code != 0 && envelope.Code != 200 {
		return nil, fmt.Errorf("%s: API error code %d: %s", endpoint, envelope.Code, envelope.Msg)
	}

	return envelope.Data, nil
}

// GetLastPowerData retrieves real-time power data for the given serial number.
func (c *Client) GetLastPowerData(ctx context.Context, serial string) (*PowerData, error) {
	data, err := c.doGet(ctx, "getLastPowerData", map[string]string{"sysSn": serial})
	if err != nil {
		return nil, err
	}

	var result PowerData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("getLastPowerData: unmarshal data: %w", err)
	}
	return &result, nil
}

// GetOneDayPower retrieves 5-minute power snapshots for the given serial and date.
func (c *Client) GetOneDayPower(ctx context.Context, serial, date string) ([]PowerSnapshot, error) {
	data, err := c.doGet(ctx, "getOneDayPowerBySn", map[string]string{"sysSn": serial, "queryDate": date})
	if err != nil {
		return nil, err
	}

	var result []PowerSnapshot
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("getOneDayPowerBySn: unmarshal data: %w", err)
	}
	return result, nil
}

// GetOneDateEnergy retrieves daily energy totals for the given serial and date.
func (c *Client) GetOneDateEnergy(ctx context.Context, serial, date string) (*EnergyData, error) {
	data, err := c.doGet(ctx, "getOneDateEnergyBySn", map[string]string{"sysSn": serial, "queryDate": date})
	if err != nil {
		return nil, err
	}

	var result EnergyData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("getOneDateEnergyBySn: unmarshal data: %w", err)
	}
	return &result, nil
}

// GetEssList retrieves system information and filters to the given serial number.
// Returns an error if the serial is not found in the response.
func (c *Client) GetEssList(ctx context.Context, serial string) (*SystemInfo, error) {
	data, err := c.doGet(ctx, "getEssList", nil)
	if err != nil {
		return nil, err
	}

	var systems []SystemInfo
	if err := json.Unmarshal(data, &systems); err != nil {
		return nil, fmt.Errorf("getEssList: unmarshal data: %w", err)
	}

	for i := range systems {
		if systems[i].SysSn == serial {
			return &systems[i], nil
		}
	}
	return nil, fmt.Errorf("getEssList: serial %q not found in %d systems", serial, len(systems))
}
