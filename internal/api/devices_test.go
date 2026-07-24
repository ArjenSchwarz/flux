package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDeviceStore implements the writer + reader interface the handler
// expects. It enforces the tzUpdatedAt monotonic guard inline so the
// production guard test exercises the real code path, not the mock.
type fakeDeviceStore struct {
	mu      sync.Mutex
	devices map[string]dynamo.DeviceItem
	putErr  error
}

func newFakeDeviceStore() *fakeDeviceStore {
	return &fakeDeviceStore{devices: make(map[string]dynamo.DeviceItem)}
}

func (s *fakeDeviceStore) GetDevice(_ context.Context, deviceID string) (*dynamo.DeviceItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.devices[deviceID]; ok {
		c := d
		return &c, nil
	}
	return nil, nil
}

func (s *fakeDeviceStore) PutDeviceConditional(_ context.Context, item dynamo.DeviceItem, incomingTZUpdatedAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return s.putErr
	}
	if existing, ok := s.devices[item.DeviceID]; ok {
		if incomingTZUpdatedAt > 0 && incomingTZUpdatedAt < existing.TZUpdatedAt {
			return &types.ConditionalCheckFailedException{}
		}
	}
	s.devices[item.DeviceID] = item
	return nil
}

func newDeviceTestHandler(store *fakeDeviceStore) *Handler {
	h := newTestHandlerFor(&mockReader{}, nil, testSerial, testToken)
	h.devices = store
	return h
}

func makeDeviceJSONRequest(method, path, body string) events.LambdaFunctionURLRequest {
	req := makeRequest(method, path, "Bearer "+testToken)
	req.Body = body
	req.Headers["content-type"] = "application/json"
	return req
}

func TestHandleRegisterDevice_Valid(t *testing.T) {
	store := newFakeDeviceStore()
	h := newDeviceTestHandler(store)

	body := `{
		"deviceId":"dev-1",
		"platform":"ios",
		"apnsToken":"deadbeef",
		"tzIdentifier":"Australia/Sydney",
		"tzUpdatedAt":100
	}`
	req := makeDeviceJSONRequest(http.MethodPost, "/devices", body)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var got dynamo.DeviceItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	assert.Equal(t, "dev-1", got.DeviceID)
	assert.Equal(t, "ios", got.Platform)
	assert.Equal(t, "deadbeef", got.APNsToken)
	assert.Equal(t, "Australia/Sydney", got.TZIdentifier)
	assert.Equal(t, int64(100), got.TZUpdatedAt)
}

func TestHandleRegisterDevice_PersistsAPNsEnvironment(t *testing.T) {
	store := newFakeDeviceStore()
	h := newDeviceTestHandler(store)

	body := `{
		"deviceId":"dev-1",
		"platform":"ios",
		"apnsToken":"deadbeef",
		"apnsEnvironment":"production",
		"tzIdentifier":"Australia/Sydney",
		"tzUpdatedAt":100
	}`
	req := makeDeviceJSONRequest(http.MethodPost, "/devices", body)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	got, _ := store.GetDevice(context.Background(), "dev-1")
	require.NotNil(t, got)
	assert.Equal(t, "production", got.APNsEnvironment,
		"apnsEnvironment must be persisted so the poller can dispatch to the right APNs host")
}

func TestHandleRegisterDevice_RejectsUnknownEnvironment(t *testing.T) {
	store := newFakeDeviceStore()
	h := newDeviceTestHandler(store)

	body := `{
		"deviceId":"dev-1",
		"platform":"ios",
		"apnsToken":"deadbeef",
		"apnsEnvironment":"staging",
		"tzIdentifier":"Australia/Sydney",
		"tzUpdatedAt":100
	}`
	req := makeDeviceJSONRequest(http.MethodPost, "/devices", body)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleRegisterDevice_PreservesEnvironmentWhenAbsent(t *testing.T) {
	// Token-less re-registration from the denial path: the payload omits
	// apnsEnvironment because the app didn't have a token to send yet.
	// The stored env from the previous successful registration must survive.
	store := newFakeDeviceStore()
	store.devices["dev-1"] = dynamo.DeviceItem{
		DeviceID:        "dev-1",
		Platform:        "ios",
		APNsEnvironment: "production",
		TZUpdatedAt:     50,
	}
	h := newDeviceTestHandler(store)

	body := `{"deviceId":"dev-1","platform":"ios","tzIdentifier":"Australia/Sydney","tzUpdatedAt":100}`
	resp, err := h.Handle(context.Background(), makeDeviceJSONRequest(http.MethodPost, "/devices", body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	got, _ := store.GetDevice(context.Background(), "dev-1")
	require.NotNil(t, got)
	assert.Equal(t, "production", got.APNsEnvironment,
		"existing env must survive a token-less re-registration")
}

func TestHandleRegisterDevice_NullToken(t *testing.T) {
	store := newFakeDeviceStore()
	h := newDeviceTestHandler(store)

	body := `{
		"deviceId":"dev-1",
		"platform":"ios",
		"tzIdentifier":"Australia/Sydney",
		"tzUpdatedAt":100
	}`
	req := makeDeviceJSONRequest(http.MethodPost, "/devices", body)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "missing token is allowed (permission denied path)")
}

func TestHandleRegisterDevice_MalformedJSON(t *testing.T) {
	store := newFakeDeviceStore()
	h := newDeviceTestHandler(store)
	req := makeDeviceJSONRequest(http.MethodPost, "/devices", `{ not json`)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleRegisterDevice_MissingDeviceID(t *testing.T) {
	store := newFakeDeviceStore()
	h := newDeviceTestHandler(store)
	body := `{"platform":"ios","tzIdentifier":"Australia/Sydney","tzUpdatedAt":1}`
	req := makeDeviceJSONRequest(http.MethodPost, "/devices", body)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleRegisterDevice_InvalidPlatform(t *testing.T) {
	store := newFakeDeviceStore()
	h := newDeviceTestHandler(store)
	body := `{"deviceId":"d","platform":"android","tzIdentifier":"UTC","tzUpdatedAt":1}`
	req := makeDeviceJSONRequest(http.MethodPost, "/devices", body)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleRegisterDevice_MissingTZ(t *testing.T) {
	store := newFakeDeviceStore()
	h := newDeviceTestHandler(store)
	body := `{"deviceId":"d","platform":"ios","tzUpdatedAt":1}`
	req := makeDeviceJSONRequest(http.MethodPost, "/devices", body)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleRegisterDevice_TZUpdatedAtMonotonicGuard(t *testing.T) {
	store := newFakeDeviceStore()
	h := newDeviceTestHandler(store)

	// First registration at tzUpdatedAt=100 succeeds.
	body1 := `{"deviceId":"dev-1","platform":"ios","tzIdentifier":"Australia/Sydney","tzUpdatedAt":100}`
	resp1, err := h.Handle(context.Background(), makeDeviceJSONRequest(http.MethodPost, "/devices", body1))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	// Stale registration (tzUpdatedAt=50) must not overwrite TZ. The current
	// stored TZUpdatedAt is 100; an older value loses the guard.
	body2 := `{"deviceId":"dev-1","platform":"ios","tzIdentifier":"America/New_York","tzUpdatedAt":50}`
	resp2, err := h.Handle(context.Background(), makeDeviceJSONRequest(http.MethodPost, "/devices", body2))
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp2.StatusCode,
		"stale tzUpdatedAt must be rejected with 409, not silently swallowed")

	got, _ := store.GetDevice(context.Background(), "dev-1")
	require.NotNil(t, got)
	assert.Equal(t, "Australia/Sydney", got.TZIdentifier,
		"stored TZ must remain at the newer registration")
}

func TestHandleRegisterDevice_StoreErrorReturns500(t *testing.T) {
	store := newFakeDeviceStore()
	store.putErr = errors.New("boom")
	h := newDeviceTestHandler(store)
	body := `{"deviceId":"d","platform":"ios","tzIdentifier":"UTC","tzUpdatedAt":1}`
	req := makeDeviceJSONRequest(http.MethodPost, "/devices", body)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestHandleRegisterDevice_RequiresPOST(t *testing.T) {
	store := newFakeDeviceStore()
	h := newDeviceTestHandler(store)
	resp, err := h.Handle(context.Background(), makeRequest(http.MethodGet, "/devices", "Bearer "+testToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}
