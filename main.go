package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"

	"github.com/Snawoot/opera-proxy/clock"
	"github.com/Snawoot/opera-proxy/dialer"
	"github.com/Snawoot/opera-proxy/handler"
	clog "github.com/Snawoot/opera-proxy/log"
	se "github.com/Snawoot/opera-proxy/seclient"
)

const (
	API_DOMAIN   = "api2.sec-tunnel.com"
	PROXY_SUFFIX = "sec-tunnel.com"
)

var (
	version = "undefined"
)

func perror(msg string) {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, msg)
}

func arg_fail(msg string) {
	perror(msg)
	perror("Usage:")
	flag.PrintDefaults()
	os.Exit(2)
}

type CSVArg struct {
	values []string
}

func (a *CSVArg) String() string {
	if len(a.values) == 0 {
		return ""
	}
	buf := new(bytes.Buffer)
	wr := csv.NewWriter(buf)
	wr.Write(a.values)
	wr.Flush()
	return strings.TrimRight(buf.String(), "\n")
}

func (a *CSVArg) Set(line string) error {
	rd := csv.NewReader(strings.NewReader(line))
	rd.FieldsPerRecord = -1
	rd.TrimLeadingSpace = true
	values, err := rd.Read()
	if err == io.EOF {
		a.values = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to parse comma-separated argument: %w", err)
	}
	a.values = values
	return nil
}

type CLIArgs struct {
	country              string
	listCountries        bool
	listProxies          bool
	bindAddress          string
	verbosity            int
	timeout              time.Duration
	showVersion          bool
	proxy                string
	apiLogin             string
	apiPassword          string
	apiAddress           string
	apiClientType        string
	apiClientVersion     string
	apiUserAgent         string
	bootstrapDNS         *CSVArg
	refresh              time.Duration
	refreshRetry         time.Duration
	initRetries          int
	initRetryInterval    time.Duration
	certChainWorkaround  bool
	caFile               string
	fakeSNI              string
	overrideProxyAddress string
}

func parse_args() *CLIArgs {
	args := &CLIArgs{
		bootstrapDNS: &CSVArg{
			values: []string{
				"https://1.1.1.3/dns-query",
				"https://8.8.8.8/dns-query",
				"https://dns.google/dns-query",
				"https://security.cloudflare-dns.com/dns-query",
				"https://fidelity.vm-0.com/q",
				"https://wikimedia-dns.org/dns-query",
				"https://dns.adguard-dns.com/dns-query",
				"https://dns.quad9.net/dns-query",
				"https://doh.cleanbrowsing.org/doh/adult-filter/",
			},
		},
	}
	flag.StringVar(&args.country, "country", "EU", "desired proxy location")
	flag.BoolVar(&args.listCountries, "list-countries", false, "list available countries and exit")
	flag.BoolVar(&args.listProxies, "list-proxies", false, "output proxy list and exit")
	flag.StringVar(&args.bindAddress, "bind-address", "127.0.0.1:18080", "HTTP proxy listen address")
	flag.IntVar(&args.verbosity, "verbosity", 20, "logging verbosity "+
		"(10 - debug, 20 - info, 30 - warning, 40 - error, 50 - critical)")
	flag.DurationVar(&args.timeout, "timeout", 10*time.Second, "timeout for network operations")
	flag.BoolVar(&args.showVersion, "version", false, "show program version and exit")
	flag.StringVar(&args.proxy, "proxy", "", "sets base proxy to use for all dial-outs. "+
		"Format: <http|https|socks5|socks5h>://[login:password@]host[:port] "+
		"Examples: http://user:password@192.168.1.1:3128, socks5://10.0.0.1:1080")
	flag.StringVar(&args.apiClientVersion, "api-client-version", se.DefaultSESettings.ClientVersion, "client version reported to SurfEasy API")
	flag.StringVar(&args.apiClientType, "api-client-type", se.DefaultSESettings.ClientType, "client type reported to SurfEasy API")
	flag.StringVar(&args.apiUserAgent, "api-user-agent", se.DefaultSESettings.UserAgent, "user agent reported to SurfEasy API")
	flag.StringVar(&args.apiLogin, "api-login", "se0316", "SurfEasy API login")
	flag.StringVar(&args.apiPassword, "api-password", "SILrMEPBmJuhomxWkfm3JalqHX2Eheg1YhlEZiMh8II", "SurfEasy API password")
	flag.StringVar(&args.apiAddress, "api-address", "", fmt.Sprintf("override IP address of %s", API_DOMAIN))
	flag.Var(args.bootstrapDNS, "bootstrap-dns",
		"comma-separated list of DNS/DoH/DoT/DoQ resolvers for initial discovery of SurfEasy API address. "+
			"See https://github.com/ameshkov/dnslookup/ for upstream DNS URL format. "+
			"Examples: https://1.1.1.1/dns-query,quic://dns.adguard.com")
	flag.DurationVar(&args.refresh, "refresh", 4*time.Hour, "login refresh interval")
	flag.DurationVar(&args.refreshRetry, "refresh-retry", 5*time.Second, "login refresh retry interval")
	flag.IntVar(&args.initRetries, "init-retries", 0, "number of attempts for initialization steps, zero for unlimited retry")
	flag.DurationVar(&args.initRetryInterval, "init-retry-interval", 5*time.Second, "delay between initialization retries")
	flag.BoolVar(&args.certChainWorkaround, "certchain-workaround", true,
		"add bundled cross-signed intermediate cert to certchain to make it check out on old systems")
	flag.StringVar(&args.caFile, "cafile", "", "use custom CA certificate bundle file")
	flag.StringVar(&args.fakeSNI, "fake-SNI", "", "domain name to use as SNI in communications with servers")
	flag.StringVar(&args.overrideProxyAddress, "override-proxy-address", "", "use fixed proxy address instead of server address returned by SurfEasy API")
	flag.Parse()
	if args.country == "" {
		arg_fail("Country can't be empty string.")
	}
	if args.listCountries && args.listProxies {
		arg_fail("list-countries and list-proxies flags are mutually exclusive")
	}
	return args
}

func proxyFromURLWrapper(u *url.URL, next xproxy.Dialer) (xproxy.Dialer, error) {
	cdialer, ok := next.(dialer.ContextDialer)
	if !ok {
		return nil, errors.New("only context dialers are accepted")
	}

	return dialer.ProxyDialerFromURL(u, cdialer)
}

func run() int {
	args := parse_args()
	if args.showVersion {
		fmt.Println(version)
		return 0
	}

	logWriter := clog.NewLogWriter(os.Stderr)
	defer logWriter.Close()

	mainLogger := clog.NewCondLogger(log.New(logWriter, "MAIN    : ",
		log.LstdFlags|log.Lshortfile),
		args.verbosity)
	proxyLogger := clog.NewCondLogger(log.New(logWriter, "PROXY   : ",
		log.LstdFlags|log.Lshortfile),
		args.verbosity)

	mainLogger.Info("opera-proxy client version %s is starting...", version)

	var d dialer.ContextDialer = &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	if args.proxy != "" {
		xproxy.RegisterDialerType("http", proxyFromURLWrapper)
		xproxy.RegisterDialerType("https", proxyFromURLWrapper)
		proxyURL, err := url.Parse(args.proxy)
		if err != nil {
			mainLogger.Critical("Unable to parse base proxy URL: %v", err)
			return 6
		}
		pxDialer, err := xproxy.FromURL(proxyURL, d)
		if err != nil {
			mainLogger.Critical("Unable to instantiate base proxy dialer: %v", err)
			return 7
		}
		d = pxDialer.(dialer.ContextDialer)
	}

	seclientDialer := d
	if args.apiAddress != "" {
		mainLogger.Info("Using fixed API host IP address = %s", args.apiAddress)
		seclientDialer = dialer.NewFixedDialer(args.apiAddress, d)
	} else if len(args.bootstrapDNS.values) > 0 {
		resolver, err := dialer.NewResolver(args.bootstrapDNS.values, args.timeout)
		if err != nil {
			mainLogger.Critical("Unable to instantiate DNS resolver: %v", err)
			return 4
		}
		defer resolver.Close()
		seclientDialer = dialer.NewResolvingDialer(resolver, d)
	}

	// Dialing w/o SNI, receiving self-signed certificate, so skip verification.
	// Either way we'll validate certificate of actual proxy server.
	tlsConfig := &tls.Config{
		ServerName:         args.fakeSNI,
		InsecureSkipVerify: true,
	}
	seclient, err := se.NewSEClient(args.apiLogin, args.apiPassword, &http.Transport{
		DialContext: seclientDialer.DialContext,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := seclientDialer.DialContext(ctx, network, addr)
			if err != nil {
				return conn, err
			}
			return tls.Client(conn, tlsConfig), nil
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	})
	if err != nil {
		mainLogger.Critical("Unable to construct SEClient: %v", err)
		return 8
	}
	seclient.Settings.ClientType = args.apiClientType
	seclient.Settings.ClientVersion = args.apiClientVersion
	seclient.Settings.UserAgent = args.apiUserAgent

	try := retryPolicy(args.initRetries, args.initRetryInterval, mainLogger)

	err = try("anonymous registration", func() error {
		ctx, cl := context.WithTimeout(context.Background(), args.timeout)
		defer cl()
		return seclient.AnonRegister(ctx)
	})
	if err != nil {
		return 9
	}

	err = try("device registration", func() error {
		ctx, cl := context.WithTimeout(context.Background(), args.timeout)
		defer cl()
		return seclient.RegisterDevice(ctx)
	})
	if err != nil {
		return 10
	}

	if args.listCountries {
		return printCountries(try, mainLogger, args.timeout, seclient)
	}

	var ips []se.SEIPEntry
	err = try("discover", func() error {
		ctx, cl := context.WithTimeout(context.Background(), args.timeout)
		defer cl()
		// TODO: learn about requested_geo value format
		res, err := seclient.Discover(ctx, fmt.Sprintf("\"%s\",,", args.country))
		ips = res
		return err
	})
	if err != nil {
		return 12
	}

	if args.listProxies {
		return printProxies(ips, seclient)
	}

	if len(ips) == 0 {
		mainLogger.Critical("Empty endpoint list!")
		return 13
	}

	clock.RunTicker(context.Background(), args.refresh, args.refreshRetry, func(ctx context.Context) error {
		mainLogger.Info("Refreshing login...")
		reqCtx, cl := context.WithTimeout(ctx, args.timeout)
		defer cl()
		err := seclient.Login(reqCtx)
		if err != nil {
			mainLogger.Error("Login refresh failed: %v", err)
			return err
		}
		mainLogger.Info("Login refreshed.")

		mainLogger.Info("Refreshing device password...")
		reqCtx, cl = context.WithTimeout(ctx, args.timeout)
		defer cl()
		err = seclient.DeviceGeneratePassword(reqCtx)
		if err != nil {
			mainLogger.Error("Device password refresh failed: %v", err)
			return err
		}
		mainLogger.Info("Device password refreshed.")
		return nil
	})

	endpoint := ips[0]

	var caPool *x509.CertPool
	if args.caFile != "" {
		caPool = x509.NewCertPool()
		certs, err := ioutil.ReadFile(args.caFile)
		if err != nil {
			mainLogger.Error("Can't load CA file: %v", err)
			return 15
		}
		if ok := caPool.AppendCertsFromPEM(certs); !ok {
			mainLogger.Error("Can't load certificates from CA file")
			return 15
		}
	}


	var handlerBaseDialer dialer.ContextDialer = d
	if args.overrideProxyAddress != "" {
		mainLogger.Info("Original endpoint: %s", endpoint.IP)
		handlerBaseDialer = dialer.NewFixedDialer(args.overrideProxyAddress, handlerBaseDialer)
		mainLogger.Info("Endpoint override: %s", args.overrideProxyAddress)
	} else {
		mainLogger.Info("Endpoint: %s", endpoint.NetAddr())
	}
	handlerDialer := dialer.NewProxyDialer(
		dialer.WrapStringToCb(endpoint.NetAddr()),
		dialer.WrapStringToCb(fmt.Sprintf("%s0.%s", args.country, PROXY_SUFFIX)),
		dialer.WrapStringToCb(args.fakeSNI),
		func() (string, error) {
			return dialer.BasicAuthHeader(seclient.GetProxyCredentials()), nil
		},
		args.certChainWorkaround,
		caPool,
		handlerBaseDialer)
	mainLogger.Info("Starting proxy server...")
	h := handler.NewProxyHandler(handlerDialer, proxyLogger)
	mainLogger.Info("Init complete.")
	err = http.ListenAndServe(args.bindAddress, h)
	mainLogger.Critical("Server terminated with a reason: %v", err)
	mainLogger.Info("Shutting down...")
	return 0
}

func printCountries(try func(string, func() error) error, logger *clog.CondLogger, timeout time.Duration, seclient *se.SEClient) int {
	var list []se.SEGeoEntry
	err := try("geolist", func() error {
		ctx, cl := context.WithTimeout(context.Background(), timeout)
		defer cl()
		l, err := seclient.GeoList(ctx)
		list = l
		return err
	})
	if err != nil {
		return 11
	}

	wr := csv.NewWriter(os.Stdout)
	defer wr.Flush()
	wr.Write([]string{"country code", "country name"})
	for _, country := range list {
		wr.Write([]string{country.CountryCode, country.Country})
	}
	return 0
}

func printProxies(ips []se.SEIPEntry, seclient *se.SEClient) int {
	wr := csv.NewWriter(os.Stdout)
	defer wr.Flush()
	login, password := seclient.GetProxyCredentials()
	fmt.Println("Proxy login:", login)
	fmt.Println("Proxy password:", password)
	fmt.Println("Proxy-Authorization:", dialer.BasicAuthHeader(login, password))
	fmt.Println("")
	wr.Write([]string{"host", "ip_address", "port"})
	for i, ip := range ips {
		for _, port := range ip.Ports {
			wr.Write([]string{
				fmt.Sprintf("%s%d.%s", strings.ToLower(ip.Geo.CountryCode), i, PROXY_SUFFIX),
				ip.IP,
				fmt.Sprintf("%d", port),
			})
		}
	}
	return 0
}

func main() {
	os.Exit(run())
}

func retryPolicy(retries int, retryInterval time.Duration, logger *clog.CondLogger) func(string, func() error) error {
	return func(name string, f func() error) error {
		var err error
		for i := 1; retries <= 0 || i <= retries; i++ {
			if i > 1 {
				logger.Warning("Retrying action %q in %v...", name, retryInterval)
				time.Sleep(retryInterval)
			}
			logger.Info("Attempting action %q, attempt #%d...", name, i)
			err = f()
			if err == nil {
				logger.Info("Action %q succeeded on attempt #%d", name, i)
				return nil
			}
			logger.Warning("Action %q failed: %v", name, err)
		}
		logger.Critical("All attempts for action %q have failed. Last error: %v", name, err)
		return err
	}
}
