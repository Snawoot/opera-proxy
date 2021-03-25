package seclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	dac "github.com/Snawoot/go-http-digest-auth-client"
	"golang.org/x/net/publicsuffix"
)

const (
	ANON_EMAIL_LOCALPART_BYTES       = 32
	ANON_PASSWORD_BYTES              = 20
	DEVICE_ID_BYTES                  = 20
	READ_LIMIT                 int64 = 128 * 1024
)

type SEEndpoints struct {
	RegisterSubscriber string
	SubscriberLogin    string
	RegisterDevice     string
	GeoList            string
	Discover           string
}

var DefaultSEEndpoints = SEEndpoints{
	RegisterSubscriber: "https://api.sec-tunnel.com/v4/register_subscriber",
	SubscriberLogin:    "https://api.sec-tunnel.com/v4/subscriber_login",
	RegisterDevice:     "https://api.sec-tunnel.com/v4/register_device",
	GeoList:            "https://api.sec-tunnel.com/v4/geo_list",
	Discover:           "https://api.sec-tunnel.com/v4/discover",
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
	HttpClient           *http.Client
	Settings             SESettings
	SubscriberEmail      string
	SubscriberPassword   string
	DeviceID             string
	AssignedDeviceID     string
	AssignedDeviceIDHash string
	DevicePassword       string
	rng                  *rand.Rand
}

// Instantiates SurfEasy client with default settings and given API keys.
// Optional `transport` parameter allows to override HTTP transport used
// for HTTP calls
func NewSEClient(apiUsername, apiSecret string, transport http.RoundTripper) (*SEClient, error) {
	if transport == nil {
		transport = http.DefaultTransport
	}

	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		return nil, err
	}

	rng := rand.New(RandomSource)

	device_id, err := randomCapitalHexString(rng, DEVICE_ID_BYTES)
	if err != nil {
		return nil, err
	}

	return &SEClient{
		HttpClient: &http.Client{
			Transport: dac.NewDigestTransport(apiUsername, apiSecret, transport),
			Jar:       jar,
		},
		Settings: DefaultSESettings,
		rng:      rng,
		DeviceID: device_id,
	}, nil
}

func (c *SEClient) AnonRegister(ctx context.Context) error {
	localPart, err := randomEmailLocalPart(c.rng)
	if err != nil {
		return err
	}

	c.SubscriberEmail = fmt.Sprintf("%s@%s.best.vpn", localPart, c.Settings.ClientType)

	password, err := randomCapitalHexString(c.rng, ANON_PASSWORD_BYTES)
	if err != nil {
		return err
	}
	c.SubscriberPassword = password

	return c.Register(ctx)
}

func (c *SEClient) Register(ctx context.Context) error {
	registerInput := url.Values{
		"email":    {c.SubscriberEmail},
		"password": {c.SubscriberPassword},
	}
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		c.Settings.Endpoints.RegisterSubscriber,
		strings.NewReader(registerInput.Encode()),
	)
	if err != nil {
		return err
	}
	c.populateRequest(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad http status: %s", resp.Status)
	}

	decoder := json.NewDecoder(resp.Body)
	var regRes SERegisterSubscriberResponse
	err = decoder.Decode(&regRes)
	cleanupBody(resp.Body)
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
	registerDeviceInput := url.Values{
		"client_type": {c.Settings.ClientType},
		"device_hash": {c.DeviceID},
		"device_name": {c.Settings.DeviceName},
	}
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		c.Settings.Endpoints.RegisterDevice,
		strings.NewReader(registerDeviceInput.Encode()),
	)
	if err != nil {
		return err
	}
	c.populateRequest(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad http status: %s", resp.Status)
	}

	decoder := json.NewDecoder(resp.Body)
	var regRes SERegisterDeviceResponse
	err = decoder.Decode(&regRes)
	cleanupBody(resp.Body)

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

func (c *SEClient) populateRequest(req *http.Request) {
	req.Header["SE-Client-Version"] = []string{c.Settings.ClientVersion}
	req.Header["SE-Operating-System"] = []string{c.Settings.OperatingSystem}
	req.Header["User-Agent"] = []string{c.Settings.UserAgent}
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
