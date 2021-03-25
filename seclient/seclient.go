package seclient

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	dac "github.com/Snawoot/go-http-digest-auth-client"
	"golang.org/x/net/publicsuffix"
)

const (
	ANON_EMAIL_LOCALPART_BYTES = 32
	ANON_PASSWORD_BYTES        = 20
	DEVICE_ID_BYTES            = 20
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
	DeviceHash      string
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
	DeviceHash:      "",
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
	AssignedDevideIDHash string
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

	_, err = c.HttpClient.Do(req)
	// TODO: handle response
	return nil
}

func (c *SEClient) populateRequest(req *http.Request) {
	req.Header.Set("SE-Client-Version", c.Settings.ClientVersion)
	req.Header.Set("SE-Operating-System", c.Settings.OperatingSystem)
	req.Header.Set("User-Agent", c.Settings.UserAgent)
}
