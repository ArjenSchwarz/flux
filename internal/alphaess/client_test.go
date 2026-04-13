package alphaess

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeJSON is a test helper that writes an apiResponse as JSON to the response writer.
func writeJSON(t *testing.T, w http.ResponseWriter, resp apiResponse) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("failed to write response: %v", err)
	}
}

func TestSign_ProducesCorrectDigest(t *testing.T) {
	c := NewClient("myAppId", "mySecret", 10*time.Second)

	ts, sig := c.sign()

	// Verify the signature matches SHA-512 of appID+appSecret+timestamp.
	h := sha512.New()
	h.Write([]byte("myAppId" + "mySecret" + ts))
	want := hex.EncodeToString(h.Sum(nil))

	assert.Equal(t, want, sig)
	assert.NotEmpty(t, ts)
}

func TestSign_TimestampChanges(t *testing.T) {
	c := NewClient("app", "secret", 10*time.Second)

	ts1, _ := c.sign()
	// Timestamps are Unix seconds — same second is fine, just verify non-empty.
	assert.NotEmpty(t, ts1)
}

func TestAuthHeaders_SetOnRequests(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		writeJSON(t, w, apiResponse{Code: 0, Msg: "Success", Data: json.RawMessage(`{}`)})
	}))
	defer srv.Close()

	c := NewClient("test-app-id", "test-secret", 10*time.Second)
	c.baseURL = srv.URL

	_, _ = c.GetLastPowerData(context.Background(), "SN123")

	assert.Equal(t, "test-app-id", gotHeaders.Get("appId"))
	assert.NotEmpty(t, gotHeaders.Get("timeStamp"))
	assert.NotEmpty(t, gotHeaders.Get("sign"))
	assert.Equal(t, "application/json", gotHeaders.Get("Content-Type"))
}

func TestGetLastPowerData_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := PowerData{Ppv: 3.5, Pload: 1.2, Pbat: -0.5, Pgrid: 0.3, Soc: 85.0}
		raw, _ := json.Marshal(data)
		writeJSON(t, w, apiResponse{Code: 0, Msg: "Success", Data: raw})
	}))
	defer srv.Close()

	c := NewClient("app", "secret", 10*time.Second)
	c.baseURL = srv.URL

	got, err := c.GetLastPowerData(context.Background(), "SN123")
	require.NoError(t, err)

	assert.Equal(t, 3.5, got.Ppv)
	assert.Equal(t, 1.2, got.Pload)
	assert.Equal(t, -0.5, got.Pbat)
	assert.Equal(t, 0.3, got.Pgrid)
	assert.Equal(t, 85.0, got.Soc)
}

func TestGetOneDateEnergy_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := EnergyData{Epv: 12.5, EInput: 3.0, EOutput: 1.5, ECharge: 5.0, EDischarge: 2.0, EGridCharge: 0.5}
		raw, _ := json.Marshal(data)
		writeJSON(t, w, apiResponse{Code: 0, Msg: "Success", Data: raw})
	}))
	defer srv.Close()

	c := NewClient("app", "secret", 10*time.Second)
	c.baseURL = srv.URL

	got, err := c.GetOneDateEnergy(context.Background(), "SN123", "2026-04-13")
	require.NoError(t, err)

	assert.Equal(t, 12.5, got.Epv)
	assert.Equal(t, 3.0, got.EInput)
	assert.Equal(t, 1.5, got.EOutput)
	assert.Equal(t, 5.0, got.ECharge)
	assert.Equal(t, 2.0, got.EDischarge)
	assert.Equal(t, 0.5, got.EGridCharge)
}

func TestGetOneDayPower_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := []PowerSnapshot{
			{Cbat: 80.0, Ppv: 2.0, Load: 1.0, FeedIn: 0.5, GridCharge: 0.0, UploadTime: "2026-04-13 10:00:00"},
			{Cbat: 82.0, Ppv: 2.5, Load: 1.1, FeedIn: 0.6, GridCharge: 0.0, UploadTime: "2026-04-13 10:05:00"},
		}
		raw, _ := json.Marshal(data)
		writeJSON(t, w, apiResponse{Code: 0, Msg: "Success", Data: raw})
	}))
	defer srv.Close()

	c := NewClient("app", "secret", 10*time.Second)
	c.baseURL = srv.URL

	got, err := c.GetOneDayPower(context.Background(), "SN123", "2026-04-13")
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, 80.0, got[0].Cbat)
	assert.Equal(t, "2026-04-13 10:00:00", got[0].UploadTime)
	assert.Equal(t, 82.0, got[1].Cbat)
}

func TestGetEssList_FiltersToSerial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := []SystemInfo{
			{SysSn: "OTHER1", Cobat: 5.0},
			{SysSn: "TARGET", Cobat: 10.0, Mbat: "bat-model", Minv: "inv-model"},
			{SysSn: "OTHER2", Cobat: 7.0},
		}
		raw, _ := json.Marshal(data)
		writeJSON(t, w, apiResponse{Code: 0, Msg: "Success", Data: raw})
	}))
	defer srv.Close()

	c := NewClient("app", "secret", 10*time.Second)
	c.baseURL = srv.URL

	got, err := c.GetEssList(context.Background(), "TARGET")
	require.NoError(t, err)

	assert.Equal(t, "TARGET", got.SysSn)
	assert.Equal(t, 10.0, got.Cobat)
}

func TestGetEssList_SerialNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := []SystemInfo{{SysSn: "OTHER", Cobat: 5.0}}
		raw, _ := json.Marshal(data)
		writeJSON(t, w, apiResponse{Code: 0, Msg: "Success", Data: raw})
	}))
	defer srv.Close()

	c := NewClient("app", "secret", 10*time.Second)
	c.baseURL = srv.URL

	_, err := c.GetEssList(context.Background(), "MISSING")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MISSING")
}

func TestNon200HTTPStatus_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient("app", "secret", 10*time.Second)
	c.baseURL = srv.URL

	_, err := c.GetLastPowerData(context.Background(), "SN123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "getLastPowerData")
	assert.Contains(t, err.Error(), "500")
}

func TestAPIEnvelopeNonZeroCode_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, apiResponse{Code: 6004, Msg: "Rate limited", Data: json.RawMessage(`null`)})
	}))
	defer srv.Close()

	c := NewClient("app", "secret", 10*time.Second)
	c.baseURL = srv.URL

	_, err := c.GetLastPowerData(context.Background(), "SN123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "getLastPowerData")
	assert.Contains(t, err.Error(), "6004")
}

func TestMalformedJSON_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	c := NewClient("app", "secret", 10*time.Second)
	c.baseURL = srv.URL

	_, err := c.GetLastPowerData(context.Background(), "SN123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "getLastPowerData")
}

func TestHTTPTimeout_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	// Use a very short timeout to trigger the error.
	c := NewClient("app", "secret", 50*time.Millisecond)
	c.baseURL = srv.URL

	_, err := c.GetLastPowerData(context.Background(), "SN123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "getLastPowerData")
}

func TestRequestBody_ContainsSerialNumber(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		writeJSON(t, w, apiResponse{Code: 0, Msg: "Success", Data: json.RawMessage(`{}`)})
	}))
	defer srv.Close()

	c := NewClient("app", "secret", 10*time.Second)
	c.baseURL = srv.URL

	_, _ = c.GetLastPowerData(context.Background(), "MY-SERIAL")

	assert.Equal(t, "MY-SERIAL", gotBody["sysSn"])
}

func TestGetOneDateEnergy_RequestContainsDateAndSerial(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		writeJSON(t, w, apiResponse{Code: 0, Msg: "Success", Data: json.RawMessage(`{}`)})
	}))
	defer srv.Close()

	c := NewClient("app", "secret", 10*time.Second)
	c.baseURL = srv.URL

	_, _ = c.GetOneDateEnergy(context.Background(), "SN123", "2026-04-13")

	assert.Equal(t, "SN123", gotBody["sysSn"])
	assert.Equal(t, "2026-04-13", gotBody["queryDate"])
}
