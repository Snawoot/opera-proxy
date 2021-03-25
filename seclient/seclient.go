package seclient

import (
	"encoding/hex"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"time"

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
	Endpoints       SEEndpoints
}

var DefaultSESettings = SESettings{
	ClientVersion:   "Stable 74.0.3911.232",
	ClientType:      "se0316",
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

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

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

func randomCapitalHexString(rng io.Reader, length int) (string, error) {
	b := make([]byte, length)
	_, err := rng.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
