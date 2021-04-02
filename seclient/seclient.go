package seclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"

	dac "github.com/Snawoot/go-http-digest-auth-client"
)

const (
	ANON_EMAIL_LOCALPART_BYTES       = 32
	ANON_PASSWORD_BYTES              = 20
	DEVICE_ID_BYTES                  = 20
	READ_LIMIT                 int64 = 128 * 1024
)

type SEEndpoints struct {
	RegisterSubscriber     string
	SubscriberLogin        string
	RegisterDevice         string
	DeviceGeneratePassword string
	GeoList                string
	Discover               string
}

var DefaultSEEndpoints = SEEndpoints{
	RegisterSubscriber:     "https://api.sec-tunnel.com/v4/register_subscriber",
	SubscriberLogin:        "https://api.sec-tunnel.com/v4/subscriber_login",
	RegisterDevice:         "https://api.sec-tunnel.com/v4/register_device",
	DeviceGeneratePassword: "https://api.sec-tunnel.com/v4/device_generate_password",
	GeoList:                "https://api.sec-tunnel.com/v4/geo_list",
	Discover:               "https://api.sec-tunnel.com/v4/discover",
}

type SESettings struct {
	ClientVersion   string
	ClientType      string
	DeviceName      string
	OperatingSystem string
	UserAgent       string
	Endpoints       SEEndpoints
}

var DefaultSESettings = SESettings{
	ClientVersion:   "Stable 74.0.3911.232",
	ClientType:      "se0316",
	UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/88.0.4324.192 Safari/537.36 OPR/74.0.3911.232",
	DeviceName:      "Opera-Browser-Client",
	OperatingSystem: "Windows",
	Endpoints:       DefaultSEEndpoints,
}

type SEClient struct {
	httpClient           *http.Client
	Settings             SESettings
	SubscriberEmail      string
	SubscriberPassword   string
	DeviceID             string
	AssignedDeviceID     string
	AssignedDeviceIDHash string
	DevicePassword       string
	Mux                  sync.Mutex
	rng                  *rand.Rand
}

type StrKV map[string]string

// Instantiates SurfEasy client with default settings and given API keys.
// Optional `transport` parameter allows to override HTTP transport used
// for HTTP calls
func NewSEClient(apiUsername, apiSecret string, transport http.RoundTripper) (*SEClient, error) {
	if transport == nil {
		transport = http.DefaultTransport
	}

	rng := rand.New(RandomSource)

	device_id, err := randomCapitalHexString(rng, DEVICE_ID_BYTES)
	if err != nil {
		return nil, err
	}

	jar, err := NewStdJar()
	if err != nil {
		return nil, err
	}

	res := &SEClient{
		httpClient: &http.Client{
			Jar:       jar,
			Transport: dac.NewDigestTransport(apiUsername, apiSecret, transport),
		},
		Settings: DefaultSESettings,
		rng:      rng,
		DeviceID: device_id,
	}

	return res, nil
}

func (c *SEClient) ResetCookies() error {
	c.Mux.Lock()
	defer c.Mux.Unlock()
	return c.resetCookies()
}

func (c *SEClient) resetCookies() error {
	return (c.httpClient.Jar.(*StdJar)).Reset()
}

func (c *SEClient) AnonRegister(ctx context.Context) error {
	c.Mux.Lock()
	defer c.Mux.Unlock()

	localPart, err := randomEmailLocalPart(c.rng)
	if err != nil {
		return err
	}

	c.SubscriberEmail = fmt.Sprintf("%s@%s.best.vpn", localPart, c.Settings.ClientType)
	c.SubscriberPassword = capitalHexSHA1(c.SubscriberEmail)

	return c.register(ctx)
}

func (c *SEClient) Register(ctx context.Context) error {
	c.Mux.Lock()
	defer c.Mux.Unlock()
	return c.register(ctx)
}

func (c *SEClient) register(ctx context.Context) error {
	err := c.resetCookies()
	if err != nil {
		return err
	}

	var regRes SERegisterSubscriberResponse
	err = c.rpcCall(ctx, c.Settings.Endpoints.RegisterSubscriber, StrKV{
		"email":    c.SubscriberEmail,
		"password": c.SubscriberPassword,
	}, &regRes)
	if err != nil {
		return err
	}

	if regRes.Status.Code != SE_STATUS_OK {
		return fmt.Errorf("API responded with error message: code=%d, msg=\"%s\"",
			regRes.Status.Code, regRes.Status.Message)
	}
	return nil
}

func (c *SEClient) RegisterDevice(ctx context.Context) error {
	c.Mux.Lock()
	defer c.Mux.Unlock()

	var regRes SERegisterDeviceResponse
	err := c.rpcCall(ctx, c.Settings.Endpoints.RegisterDevice, StrKV{
		"client_type": c.Settings.ClientType,
		"device_hash": c.DeviceID,
		"device_name": c.Settings.DeviceName,
	}, &regRes)
	if err != nil {
		return err
	}

	if regRes.Status.Code != SE_STATUS_OK {
		return fmt.Errorf("API responded with error message: code=%d, msg=\"%s\"",
			regRes.Status.Code, regRes.Status.Message)
	}

	c.AssignedDeviceID = regRes.Data.DeviceID
	c.DevicePassword = regRes.Data.DevicePassword
	c.AssignedDeviceIDHash = capitalHexSHA1(regRes.Data.DeviceID)
	return nil
}

func (c *SEClient) GeoList(ctx context.Context) ([]SEGeoEntry, error) {
	c.Mux.Lock()
	defer c.Mux.Unlock()

	var geoListRes SEGeoListResponse
	err := c.rpcCall(ctx, c.Settings.Endpoints.GeoList, StrKV{
		"device_id": c.AssignedDeviceIDHash,
	}, &geoListRes)
	if err != nil {
		return nil, err
	}

	if geoListRes.Status.Code != SE_STATUS_OK {
		return nil, fmt.Errorf("API responded with error message: code=%d, msg=\"%s\"",
			geoListRes.Status.Code, geoListRes.Status.Message)
	}

	return geoListRes.Data.Geos, nil
}

func (c *SEClient) Discover(ctx context.Context, requestedGeo string) ([]SEIPEntry, error) {
	c.Mux.Lock()
	defer c.Mux.Unlock()

	var discoverRes SEDiscoverResponse
	err := c.rpcCall(ctx, c.Settings.Endpoints.Discover, StrKV{
		"serial_no":     c.AssignedDeviceIDHash,
		"requested_geo": requestedGeo,
	}, &discoverRes)
	if err != nil {
		return nil, err
	}

	if discoverRes.Status.Code != SE_STATUS_OK {
		return nil, fmt.Errorf("API responded with error message: code=%d, msg=\"%s\"",
			discoverRes.Status.Code, discoverRes.Status.Message)
	}

	return discoverRes.Data.IPs, nil
}

func (c *SEClient) Login(ctx context.Context) error {
	c.Mux.Lock()
	defer c.Mux.Unlock()

	err := c.resetCookies()
	if err != nil {
		return err
	}

	var loginRes SESubscriberLoginResponse
	err = c.rpcCall(ctx, c.Settings.Endpoints.SubscriberLogin, StrKV{
		"login":       c.SubscriberEmail,
		"password":    c.SubscriberPassword,
		"client_type": c.Settings.ClientType,
	}, &loginRes)
	if err != nil {
		return err
	}

	if loginRes.Status.Code != SE_STATUS_OK {
		return fmt.Errorf("API responded with error message: code=%d, msg=\"%s\"",
			loginRes.Status.Code, loginRes.Status.Message)
	}
	return nil
}

func (c *SEClient) DeviceGeneratePassword(ctx context.Context) error {
	c.Mux.Lock()
	defer c.Mux.Unlock()

	var genRes SEDeviceGeneratePasswordResponse
	err := c.rpcCall(ctx, c.Settings.Endpoints.DeviceGeneratePassword, StrKV{
		"device_id": c.AssignedDeviceID,
	}, &genRes)
	if err != nil {
		return err
	}

	if genRes.Status.Code != SE_STATUS_OK {
		return fmt.Errorf("API responded with error message: code=%d, msg=\"%s\"",
			genRes.Status.Code, genRes.Status.Message)
	}

	c.DevicePassword = genRes.Data.DevicePassword
	return nil
}

func (c *SEClient) GetProxyCredentials() (string, string) {
	c.Mux.Lock()
	defer c.Mux.Unlock()

	return c.AssignedDeviceIDHash, c.DevicePassword
}

func (c *SEClient) populateRequest(req *http.Request) {
	req.Header["SE-Client-Version"] = []string{c.Settings.ClientVersion}
	req.Header["SE-Operating-System"] = []string{c.Settings.OperatingSystem}
	req.Header["User-Agent"] = []string{c.Settings.UserAgent}
}

func (c *SEClient) RpcCall(ctx context.Context, endpoint string, params map[string]string, res interface{}) error {
	c.Mux.Lock()
	defer c.Mux.Unlock()

	return c.rpcCall(ctx, endpoint, params, res)
}

func (c *SEClient) rpcCall(ctx context.Context, endpoint string, params map[string]string, res interface{}) error {
	input := make(url.Values)
	for k, v := range params {
		input[k] = []string{v}
	}
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		endpoint,
		strings.NewReader(input.Encode()),
	)
	if err != nil {
		return err
	}
	c.populateRequest(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad http status: %s, headers: %#v", resp.Status, resp.Header)
	}

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(res)
	cleanupBody(resp.Body)

	if err != nil {
		return err
	}

	return nil
}

// Does cleanup of HTTP response in order to make it reusable by keep-alive
// logic of HTTP client
func cleanupBody(body io.ReadCloser) {
	io.Copy(ioutil.Discard, &io.LimitedReader{
		R: body,
		N: READ_LIMIT,
	})
	body.Close()
}
