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
	"strconv"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"

	"github.com/Snawoot/opera-proxy/clock"
	"github.com/Snawoot/opera-proxy/dialer"
	"github.com/Snawoot/opera-proxy/handler"
	clog "github.com/Snawoot/opera-proxy/log"
	"github.com/Snawoot/opera-proxy/resolver"
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

type serverSelectionArg struct {
	value dialer.ServerSelection
}

func (a *serverSelectionArg) Set(s string) error {
	v, err := dialer.ParseServerSelection(s)
	if err != nil {
		return err
	}
	a.value = v
	return nil
}

func (a *serverSelectionArg) String() string {
	return a.value.String()
}

type CLIArgs struct {
	country                string
	listCountries          bool
	listProxies            bool
	dpExport               bool
	bindAddress            string
	socksMode              bool
	verbosity              int
	timeout                time.Duration
	showVersion            bool
	proxy                  string
	apiLogin               string
	apiPassword            string
	apiAddress             string
	apiClientType          string
	apiClientVersion       string
	apiUserAgent           string
	apiProxy               string
	bootstrapDNS           *CSVArg
	refresh                time.Duration
	refreshRetry           time.Duration
	initRetries            int
	initRetryInterval      time.Duration
	certChainWorkaround    bool
	caFile                 string
	fakeSNI                string
	overrideProxyAddress   string
	serverSelection        serverSelectionArg
	serverSelectionTimeout time.Duration
	serverSelectionTestURL string
	serverSelectionDLLimit int64
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
		serverSelection: serverSelectionArg{dialer.ServerSelectionFastest},
	}
	flag.StringVar(&args.country, "country", "EU", "desired proxy location")
	flag.BoolVar(&args.listCountries, "list-countries", false, "list available countries and exit")
	flag.BoolVar(&args.listProxies, "list-proxies", false, "output proxy list and exit")
	flag.BoolVar(&args.dpExport, "dp-export", false, "export configuration for dumbproxy")
	flag.StringVar(&args.bindAddress, "bind-address", "127.0.0.1:18080", "proxy listen address")
	flag.BoolVar(&args.socksMode, "socks-mode", false, "listen for SOCKS requests instead of HTTP")
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
	flag.StringVar(&args.apiProxy, "api-proxy", "", "additional proxy server used to access SurfEasy API")
	flag.Var(args.bootstrapDNS, "bootstrap-dns",
		"comma-separated list of DNS/DoH/DoT resolvers for initial discovery of SurfEasy API address. "+
			"Supported schemes are: dns://, https://, tls://, tcp://. "+
			"Examples: https://1.1.1.1/dns-query,tls://9.9.9.9:853")
	flag.DurationVar(&args.refresh, "refresh", 4*time.Hour, "login refresh interval")
	flag.DurationVar(&args.refreshRetry, "refresh-retry", 5*time.Second, "login refresh retry interval")
	flag.IntVar(&args.initRetries, "init-retries", 0, "number of attempts for initialization steps, zero for unlimited retry")
	flag.DurationVar(&args.initRetryInterval, "init-retry-interval", 5*time.Second, "delay between initialization retries")
	flag.BoolVar(&args.certChainWorkaround, "certchain-workaround", true,
		"add bundled cross-signed intermediate cert to certchain to make it check out on old systems")
	flag.StringVar(&args.caFile, "cafile", "", "use custom CA certificate bundle file")
	flag.StringVar(&args.fakeSNI, "fake-SNI", "", "domain name to use as SNI in communications with servers")
	flag.StringVar(&args.overrideProxyAddress, "override-proxy-address", "", "use fixed proxy address instead of server address returned by SurfEasy API")
	flag.Var(&args.serverSelection, "server-selection", "server selection policy (first/random/fastest)")
	flag.DurationVar(&args.serverSelectionTimeout, "server-selection-timeout", 30*time.Second, "timeout given for server selection function to produce result")
	flag.StringVar(&args.serverSelectionTestURL, "server-selection-test-url", "https://ajax.googleapis.com/ajax/libs/angularjs/1.8.2/angular.min.js",
		"URL used for download benchmark by fastest server selection policy")
	flag.Int64Var(&args.serverSelectionDLLimit, "server-selection-dl-limit", 0, "restrict amount of downloaded data per connection by fastest server selection")
	flag.Parse()
	if args.country == "" {
		arg_fail("Country can't be empty string.")
	}
	if args.listCountries && args.listProxies || args.listCountries && args.dpExport || args.listProxies && args.dpExport {
		arg_fail("mutually exclusive output arguments were provided")
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
	socksLogger := log.New(logWriter, "SOCKS   : ",
		log.LstdFlags|log.Lshortfile)

	mainLogger.Info("opera-proxy client version %s is starting...", version)

	var d dialer.ContextDialer = &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

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

	xproxy.RegisterDialerType("http", proxyFromURLWrapper)
	xproxy.RegisterDialerType("https", proxyFromURLWrapper)
	if args.proxy != "" {
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
	if args.apiProxy != "" {
		apiProxyURL, err := url.Parse(args.apiProxy)
		if err != nil {
			mainLogger.Critical("Unable to parse base proxy URL: %v", err)
			return 6
		}
		pxDialer, err := xproxy.FromURL(apiProxyURL, seclientDialer)
		if err != nil {
			mainLogger.Critical("Unable to instantiate base proxy dialer: %v", err)
			return 7
		}
		seclientDialer = pxDialer.(dialer.ContextDialer)
	}
	if args.apiAddress != "" {
		mainLogger.Info("Using fixed API host address = %s", args.apiAddress)
		seclientDialer = dialer.NewFixedDialer(args.apiAddress, seclientDialer)
	} else if len(args.bootstrapDNS.values) > 0 {
		resolver, err := resolver.FastFromURLs(args.bootstrapDNS.values...)
		if err != nil {
			mainLogger.Critical("Unable to instantiate DNS resolver: %v", err)
			return 4
		}
		seclientDialer = dialer.NewResolvingDialer(resolver, seclientDialer)
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
	if args.listProxies || args.dpExport {
		err = try("discover", func() error {
			ctx, cl := context.WithTimeout(context.Background(), args.timeout)
			defer cl()
			ips, err = seclient.Discover(ctx, fmt.Sprintf("\"%s\",,", args.country))
			if err != nil {
				return err
			}
			if len(ips) == 0 {
				return errors.New("empty endpoints list!")
			}
			return nil
		})
		if err != nil {
			return 12
		}
		if args.listProxies {
			return printProxies(ips, seclient)
		}
		if args.dpExport {
			return dpExport(ips, seclient)
		}
	}

	handlerDialerFactory := func(endpointAddr string) dialer.ContextDialer {
		return dialer.NewProxyDialer(
			dialer.WrapStringToCb(endpointAddr),
			dialer.WrapStringToCb(fmt.Sprintf("%s0.%s", args.country, PROXY_SUFFIX)),
			dialer.WrapStringToCb(args.fakeSNI),
			func() (string, error) {
				return dialer.BasicAuthHeader(seclient.GetProxyCredentials()), nil
			},
			args.certChainWorkaround,
			caPool,
			d)
	}

	var handlerDialer dialer.ContextDialer

	if args.overrideProxyAddress == "" {
		err = try("discover", func() error {
			ctx, cl := context.WithTimeout(context.Background(), args.timeout)
			defer cl()
			res, err := seclient.Discover(ctx, fmt.Sprintf("\"%s\",,", args.country))
			if err != nil {
				return err
			}
			if len(res) == 0 {
				return errors.New("empty endpoints list!")
			}

			mainLogger.Info("Discovered endpoints: %v. Starting server selection routine %q.", res, args.serverSelection.value)
			var ss dialer.SelectionFunc
			switch args.serverSelection.value {
			case dialer.ServerSelectionFirst:
				ss = dialer.SelectFirst
			case dialer.ServerSelectionRandom:
				ss = dialer.SelectRandom
			case dialer.ServerSelectionFastest:
				ss = dialer.NewFastestServerSelectionFunc(
					args.serverSelectionTestURL,
					args.serverSelectionDLLimit,
					&tls.Config{
						RootCAs: caPool,
					},
				)
			default:
				panic("unhandled server selection value got past parsing")
			}
			dialers := make([]dialer.ContextDialer, len(res))
			for i, ep := range res {
				dialers[i] = handlerDialerFactory(ep.NetAddr())
			}
			ctx, cl = context.WithTimeout(context.Background(), args.serverSelectionTimeout)
			defer cl()
			handlerDialer, err = ss(ctx, dialers)
			if err != nil {
				return err
			}
			if addresser, ok := handlerDialer.(interface{ Address() (string, error) }); ok {
				if epAddr, err := addresser.Address(); err == nil {
					mainLogger.Info("Selected endpoint address: %s", epAddr)
				}
			}
			return nil
		})
		if err != nil {
			return 12
		}
	} else {
		sanitizedEndpoint := sanitizeFixedProxyAddress(args.overrideProxyAddress)
		handlerDialer = handlerDialerFactory(sanitizedEndpoint)
		mainLogger.Info("Endpoint override: %s", sanitizedEndpoint)
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

	mainLogger.Info("Starting proxy server...")
	if args.socksMode {
		socks, initError := handler.NewSocksServer(handlerDialer, socksLogger)
		if initError != nil {
			mainLogger.Critical("Failed to start: %v", err)
			return 16
		}
		mainLogger.Info("Init complete.")
		err = socks.ListenAndServe("tcp", args.bindAddress)
	} else {
		h := handler.NewProxyHandler(handlerDialer, proxyLogger)
		mainLogger.Info("Init complete.")
		err = http.ListenAndServe(args.bindAddress, h)
	}
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

func dpExport(ips []se.SEIPEntry, seclient *se.SEClient) int {
	wr := csv.NewWriter(os.Stdout)
	wr.Comma = ' '
	defer wr.Flush()
	creds := url.UserPassword(seclient.GetProxyCredentials())
	var gotOne bool
	for i, ip := range ips {
		if len(ip.Ports) == 0 {
			continue
		}
		u := url.URL{
			Scheme: "https",
			User:   creds,
			Host: net.JoinHostPort(
				ip.IP,
				strconv.Itoa(int(ip.Ports[0])),
			),
			RawQuery: url.Values{
				"sni":      []string{""},
				"peername": []string{fmt.Sprintf("%s%d.%s", strings.ToLower(ip.Geo.CountryCode), i, PROXY_SUFFIX)},
			}.Encode(),
		}
		key := "proxy"
		if gotOne {
			key = "#proxy"
		}
		wr.Write([]string{
			key,
			u.String(),
		})
		gotOne = true
	}
	return 0
}

func sanitizeFixedProxyAddress(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, "443")
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
