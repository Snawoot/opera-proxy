package seclient

import (
	"net/http"
	"net/http/cookiejar"

	dac "github.com/Snawoot/go-http-digest-auth-client"
	"golang.org/x/net/publicsuffix"
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
	OperatingSystem string
	APIUsername     string
	APISecret       string
	Endpoints       SEEndpoints
}

var DefaultSESettings = SESettings{
	ClientVersion:   "Stable 74.0.3911.232",
	OperatingSystem: "Windows",
	Endpoints:       DefaultSEEndpoints,
}

type SEClient struct {
	HttpClient *http.Client
	Settings SESettings
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

	return &SEClient{
		HttpClient: &http.Client{
			Transport: dac.NewDigestTransport(apiUsername, apiSecret, transport),
			Jar: jar,
		},
		Settings: DefaultSESettings,
	}, nil
}
