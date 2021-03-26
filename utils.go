package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const COPY_BUF = 128 * 1024

func basic_auth_header(login, password string) string {
	return "basic " + base64.StdEncoding.EncodeToString(
		[]byte(login+":"+password))
}

func proxy(ctx context.Context, left, right net.Conn) {
	wg := sync.WaitGroup{}
	cpy := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		dst.Close()
	}
	wg.Add(2)
	go cpy(left, right)
	go cpy(right, left)
	groupdone := make(chan struct{})
	go func() {
		wg.Wait()
		groupdone <- struct{}{}
	}()
	select {
	case <-ctx.Done():
		left.Close()
		right.Close()
	case <-groupdone:
		return
	}
	<-groupdone
	return
}

func proxyh2(ctx context.Context, leftreader io.ReadCloser, leftwriter io.Writer, right net.Conn) {
	wg := sync.WaitGroup{}
	ltr := func(dst net.Conn, src io.Reader) {
		defer wg.Done()
		io.Copy(dst, src)
		dst.Close()
	}
	rtl := func(dst io.Writer, src io.Reader) {
		defer wg.Done()
		copyBody(dst, src)
	}
	wg.Add(2)
	go ltr(right, leftreader)
	go rtl(leftwriter, right)
	groupdone := make(chan struct{}, 1)
	go func() {
		wg.Wait()
		groupdone <- struct{}{}
	}()
	select {
	case <-ctx.Done():
		leftreader.Close()
		right.Close()
	case <-groupdone:
		return
	}
	<-groupdone
	return
}

func print_countries(timeout time.Duration) int {
	var (
		countries CountryList
		err       error
	)
	tx_res, tx_err := EnsureTransaction(context.Background(), timeout, func(ctx context.Context, client *http.Client) bool {
		countries, err = VPNCountries(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Transaction error: %v. Retrying with the fallback mechanism...\n", err)
			return false
		}
		return true
	})
	if tx_err != nil {
		fmt.Fprintf(os.Stderr, "Transaction recovery mechanism failure: %v.\n", tx_err)
		return 4
	}
	if !tx_res {
		fmt.Fprintf(os.Stderr, "All attempts failed.")
		return 3
	}
	for _, code := range countries {
		fmt.Printf("%v - %v\n", code, ISO3166[strings.ToUpper(code)])
	}
	return 0
}

func print_proxies(country string, proxy_type string, limit uint, timeout time.Duration) int {
	var (
		tunnels   *ZGetTunnelsResponse
		user_uuid string
		err       error
	)
	tx_res, tx_err := EnsureTransaction(context.Background(), timeout, func(ctx context.Context, client *http.Client) bool {
		tunnels, user_uuid, err = Tunnels(ctx, client, country, proxy_type, limit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Transaction error: %v. Retrying with the fallback mechanism...\n", err)
			return false
		}
		return true
	})
	if tx_err != nil {
		fmt.Fprintf(os.Stderr, "Transaction recovery mechanism failure: %v.\n", tx_err)
		return 4
	}
	if !tx_res {
		fmt.Fprintf(os.Stderr, "All attempts failed.")
		return 3
	}
	wr := csv.NewWriter(os.Stdout)
	login := LOGIN_PREFIX + user_uuid
	password := tunnels.AgentKey
	fmt.Println("Login:", login)
	fmt.Println("Password:", password)
	fmt.Println("Proxy-Authorization:",
		basic_auth_header(login, password))
	fmt.Println("")
	wr.Write([]string{"host", "ip_address", "direct", "peer", "hola", "trial", "trial_peer", "vendor"})
	for host, ip := range tunnels.IPList {
		if PROTOCOL_WHITELIST[tunnels.Protocol[host]] {
			wr.Write([]string{host,
				ip,
				strconv.FormatUint(uint64(tunnels.Port.Direct), 10),
				strconv.FormatUint(uint64(tunnels.Port.Peer), 10),
				strconv.FormatUint(uint64(tunnels.Port.Hola), 10),
				strconv.FormatUint(uint64(tunnels.Port.Trial), 10),
				strconv.FormatUint(uint64(tunnels.Port.TrialPeer), 10),
				tunnels.Vendor[host]})
		}
	}
	wr.Flush()
	return 0
}

// Hop-by-hop headers. These are removed when sent to the backend.
// http://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html
var hopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Connection",
	"Te", // canonicalized version of "TE"
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func delHopHeaders(header http.Header) {
	for _, h := range hopHeaders {
		header.Del(h)
	}
}

func hijack(hijackable interface{}) (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := hijackable.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("Connection doesn't support hijacking")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, nil, err
	}
	var emptytime time.Time
	err = conn.SetDeadline(emptytime)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, rw, nil
}

func flush(flusher interface{}) bool {
	f, ok := flusher.(http.Flusher)
	if !ok {
		return false
	}
	f.Flush()
	return true
}

func copyBody(wr io.Writer, body io.Reader) {
	buf := make([]byte, COPY_BUF)
	for {
		bread, read_err := body.Read(buf)
		var write_err error
		if bread > 0 {
			_, write_err = wr.Write(buf[:bread])
			flush(wr)
		}
		if read_err != nil || write_err != nil {
			break
		}
	}
}
