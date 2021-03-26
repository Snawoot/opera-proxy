package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	xproxy "golang.org/x/net/proxy"
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
	country      string
	bind_address string
	verbosity    int
	timeout      time.Duration
	resolver     string
	showVersion  bool
	proxy        string
}

func parse_args() CLIArgs {
	var args CLIArgs
	flag.StringVar(&args.country, "country", "EU", "desired proxy location")
	flag.BoolVar(&args.list_countries, "list-countries", false, "list available countries and exit")
	flag.BoolVar(&args.list_proxies, "list-proxies", false, "output proxy list and exit")
	flag.StringVar(&args.bind_address, "bind-address", "127.0.0.1:8080", "HTTP proxy listen address")
	flag.IntVar(&args.verbosity, "verbosity", 20, "logging verbosity "+
		"(10 - debug, 20 - info, 30 - warning, 40 - error, 50 - critical)")
	flag.DurationVar(&args.timeout, "timeout", 10*time.Second, "timeout for network operations")
	flag.StringVar(&args.resolver, "resolver", "https://cloudflare-dns.com/dns-query",
		"DNS/DoH/DoT resolver to workaround Hola blocked hosts. "+
			"See https://github.com/ameshkov/dnslookup/ for upstream DNS URL format.")
	flag.BoolVar(&args.showVersion, "version", false, "show program version and exit")
	flag.StringVar(&args.proxy, "proxy", "", "sets base proxy to use for all dial-outs. "+
		"Format: <http|https|socks5|socks5h>://[login:password@]host[:port] "+
		"Examples: http://user:password@192.168.1.1:3128, socks5://10.0.0.1:1080")
	flag.Parse()
	if args.country == "" {
		arg_fail("Country can't be empty string.")
	}
	if args.list_countries && args.list_proxies {
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

	if args.list_countries {
		return print_countries(args.timeout)
	}
	if args.list_proxies {
		return print_proxies(args.country, args.proxy_type, args.limit, args.timeout)
	}

	mainLogger.Info("opera-proxy client version %s is starting...", version)
	mainLogger.Info("Constructing fallback DNS upstream...")
	resolver, err := NewResolver(args.resolver, args.timeout)
	if err != nil {
		mainLogger.Critical("Unable to instantiate DNS resolver: %v", err)
		return 6
	}

	// TODO: get creds here
	handlerDialer := NewProxyDialer(endpoint.NetAddr(), endpoint.TLSName, auth, dialer)
	mainLogger.Info("Endpoint: %s", endpoint.URL().String())
	mainLogger.Info("Starting proxy server...")
	handler := NewProxyHandler(handlerDialer, proxyLogger)
	mainLogger.Info("Init complete.")
	err = http.ListenAndServe(args.bind_address, handler)
	mainLogger.Critical("Server terminated with a reason: %v", err)
	mainLogger.Info("Shutting down...")
	return 0
}

func main() {
	os.Exit(run())
}
