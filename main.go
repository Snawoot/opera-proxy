package main

import (
	"context"
	"crypto/tls"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
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

type CLIArgs struct {
	country       string
	listCountries bool
	listProxies   bool
	bindAddress   string
	verbosity     int
	timeout       time.Duration
	showVersion   bool
	proxy         string
	apiLogin      string
	apiPassword   string
	apiAddress    string
	bootstrapDNS  string
	refresh       time.Duration
}

func parse_args() CLIArgs {
	var args CLIArgs
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
	flag.StringVar(&args.bootstrapDNS, "bootstrap-dns", "",
		"DNS/DoH/DoT/DoQ resolver for initial discovering of SurfEasy API address. "+
			"See https://github.com/ameshkov/dnslookup/ for upstream DNS URL format. "+
			"Examples: https://1.1.1.1/dns-query, quic://dns.adguard.com")
	flag.DurationVar(&args.refresh, "refresh", 4*time.Hour, "login refresh interval")
	flag.Parse()
	if args.country == "" {
		arg_fail("Country can't be empty string.")
	}
	if args.listCountries && args.listProxies {
		arg_fail("list-countries and list-proxies flags are mutually exclusive")
	}
	if args.apiAddress != "" && args.bootstrapDNS != "" {
		arg_fail("api-address and bootstrap-dns options are mutually exclusive")
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
	if args.apiAddress != "" || args.bootstrapDNS != "" {
		var apiAddress string
		if args.apiAddress != "" {
			apiAddress = args.apiAddress
			mainLogger.Info("Using fixed API host IP address = %s", apiAddress)
		} else {
			resolver, err := NewResolver(args.bootstrapDNS, args.timeout)
			if err != nil {
				mainLogger.Critical("Unable to instantiate DNS resolver: %v", err)
				return 4
			}

			mainLogger.Info("Discovering API IP address...")
			addrs := resolver.ResolveA(API_DOMAIN)
			if len(addrs) == 0 {
				mainLogger.Critical("Unable to resolve %s with specified bootstrap DNS", API_DOMAIN)
				return 14
			}

			apiAddress = addrs[0]
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

	runTicker(context.Background(), args.refresh, func(ctx context.Context) {
		mainLogger.Info("Refreshing login...")
		loginCtx, cl := context.WithTimeout(ctx, args.timeout)
		defer cl()
		err := seclient.Login(loginCtx)
		if err != nil {
			mainLogger.Critical("Login refresh failed: %v", err)
			return
		}
		mainLogger.Info("Login refreshed.")
	})

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

	endpoint := ips[0]
	authHdr := basic_auth_header(seclient.GetProxyCredentials())
	auth := func() string {
		return authHdr
	}

	handlerDialer := NewProxyDialer(endpoint.NetAddr(), fmt.Sprintf("%s0.%s", args.country, PROXY_SUFFIX), auth, dialer)
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
