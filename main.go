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
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"

	se "github.com/Snawoot/opera-proxy/seclient"
)

const (
	API_DOMAIN   = "api.sec-tunnel.com"
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
	country             string
	listCountries       bool
	listProxies         bool
	bindAddress         string
	verbosity           int
	timeout             time.Duration
	showVersion         bool
	proxy               string
	apiLogin            string
	apiPassword         string
	apiAddress          string
	bootstrapDNS        *CSVArg
	refresh             time.Duration
	refreshRetry        time.Duration
	certChainWorkaround bool
	caFile              string
}

func parse_args() *CLIArgs {
	args := &CLIArgs{
		bootstrapDNS: &CSVArg{
			values: []string{
				"https://1.1.1.3/dns-query",
				"https://8.8.8.8/dns-query",
				"https://dns.google/dns-query",
				"https://security.cloudflare-dns.com/dns-query",
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
	flag.StringVar(&args.apiLogin, "api-login", "se0316", "SurfEasy API login")
	flag.StringVar(&args.apiPassword, "api-password", "SILrMEPBmJuhomxWkfm3JalqHX2Eheg1YhlEZiMh8II", "SurfEasy API password")
	flag.StringVar(&args.apiAddress, "api-address", "", fmt.Sprintf("override IP address of %s", API_DOMAIN))
	flag.Var(args.bootstrapDNS, "bootstrap-dns",
		"comma-separated list of DNS/DoH/DoT/DoQ resolvers for initial discovery of SurfEasy API address. "+
			"See https://github.com/ameshkov/dnslookup/ for upstream DNS URL format. "+
			"Examples: https://1.1.1.1/dns-query,quic://dns.adguard.com")
	flag.DurationVar(&args.refresh, "refresh", 4*time.Hour, "login refresh interval")
	flag.DurationVar(&args.refreshRetry, "refresh-retry", 5*time.Second, "login refresh retry interval")
	flag.BoolVar(&args.certChainWorkaround, "certchain-workaround", true,
		"add bundled cross-signed intermediate cert to certchain to make it check out on old systems")
	flag.StringVar(&args.caFile, "cafile", "", "use custom CA certificate bundle file")
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
	cdialer, ok := next.(ContextDialer)
	if !ok {
		return nil, errors.New("only context dialers are accepted")
	}

	return ProxyDialerFromURL(u, cdialer)
}

func run() int {
	args := parse_args()
	if args.showVersion {
		fmt.Println(version)
		return 0
	}

	logWriter := NewLogWriter(os.Stderr)
	defer logWriter.Close()

	mainLogger := NewCondLogger(log.New(logWriter, "MAIN    : ",
		log.LstdFlags|log.Lshortfile),
		args.verbosity)
	proxyLogger := NewCondLogger(log.New(logWriter, "PROXY   : ",
		log.LstdFlags|log.Lshortfile),
		args.verbosity)

	mainLogger.Info("opera-proxy client version %s is starting...", version)

	var dialer ContextDialer = &net.Dialer{
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
		pxDialer, err := xproxy.FromURL(proxyURL, dialer)
		if err != nil {
			mainLogger.Critical("Unable to instantiate base proxy dialer: %v", err)
			return 7
		}
		dialer = pxDialer.(ContextDialer)
	}

	seclientDialer := dialer
	if args.apiAddress != "" || len(args.bootstrapDNS.values) > 0 {
		var apiAddress string
		if args.apiAddress != "" {
			apiAddress = args.apiAddress
			mainLogger.Info("Using fixed API host IP address = %s", apiAddress)
		} else {
			resolver, err := NewResolver(args.bootstrapDNS.values, args.timeout)
			if err != nil {
				mainLogger.Critical("Unable to instantiate DNS resolver: %v", err)
				return 4
			}

			mainLogger.Info("Discovering API IP address...")
			addrs, err := func() ([]netip.Addr, error) {
				ctx, cancel := context.WithTimeout(context.Background(), args.timeout)
				defer cancel()
				defer func() {
					resolver = nil
				}()
				defer resolver.Close()
				return resolver.LookupNetIP(ctx, "ip4", API_DOMAIN)
			}()
			if err != nil {
				mainLogger.Critical("Unable to resolve API server address: %v", err)
				return 14
			}
			if len(addrs) == 0 {
				mainLogger.Critical("Unable to resolve %s with specified bootstrap DNS", API_DOMAIN)
				return 14
			}

			apiAddress = addrs[0].String()
			mainLogger.Info("Discovered address of API host = %s", apiAddress)
		}
		seclientDialer = NewFixedDialer(apiAddress, dialer)
	}

	// Dialing w/o SNI, receiving self-signed certificate, so skip verification.
	// Either way we'll validate certificate of actual proxy server.
	tlsConfig := &tls.Config{
		ServerName:         "",
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

	ctx, cl := context.WithTimeout(context.Background(), args.timeout)
	err = seclient.AnonRegister(ctx)
	if err != nil {
		mainLogger.Critical("Unable to perform anonymous registration: %v", err)
		return 9
	}
	cl()

	ctx, cl = context.WithTimeout(context.Background(), args.timeout)
	err = seclient.RegisterDevice(ctx)
	if err != nil {
		mainLogger.Critical("Unable to perform device registration: %v", err)
		return 10
	}
	cl()

	if args.listCountries {
		return printCountries(mainLogger, args.timeout, seclient)
	}

	ctx, cl = context.WithTimeout(context.Background(), args.timeout)
	// TODO: learn about requested_geo value format
	ips, err := seclient.Discover(ctx, fmt.Sprintf("\"%s\",,", args.country))
	if err != nil {
		mainLogger.Critical("Endpoint discovery failed: %v", err)
		return 12
	}

	if args.listProxies {
		return printProxies(ips, seclient)
	}

	if len(ips) == 0 {
		mainLogger.Critical("Empty endpoint list!")
		return 13
	}

	runTicker(context.Background(), args.refresh, args.refreshRetry, func(ctx context.Context) error {
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
	auth := func() string {
		return basic_auth_header(seclient.GetProxyCredentials())
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

	handlerDialer := NewProxyDialer(endpoint.NetAddr(), fmt.Sprintf("%s0.%s", args.country, PROXY_SUFFIX), auth, args.certChainWorkaround, caPool, dialer)
	mainLogger.Info("Endpoint: %s", endpoint.NetAddr())
	mainLogger.Info("Starting proxy server...")
	handler := NewProxyHandler(handlerDialer, proxyLogger)
	mainLogger.Info("Init complete.")
	err = http.ListenAndServe(args.bindAddress, handler)
	mainLogger.Critical("Server terminated with a reason: %v", err)
	mainLogger.Info("Shutting down...")
	return 0
}

func printCountries(logger *CondLogger, timeout time.Duration, seclient *se.SEClient) int {
	ctx, cl := context.WithTimeout(context.Background(), timeout)
	defer cl()
	list, err := seclient.GeoList(ctx)
	if err != nil {
		logger.Critical("GeoList error: %v", err)
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
	fmt.Println("Proxy-Authorization:", basic_auth_header(login, password))
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
