package dialer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

const (
	PROXY_CONNECT_METHOD       = "CONNECT"
	PROXY_HOST_HEADER          = "Host"
	PROXY_AUTHORIZATION_HEADER = "Proxy-Authorization"
)

type stringCb = func() (string, error)

type Dialer interface {
	Dial(network, address string) (net.Conn, error)
}

type ContextDialer interface {
	Dialer
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

type ProxyDialer struct {
	address       stringCb
	tlsServerName stringCb
	fakeSNI       stringCb
	auth          stringCb
	next          ContextDialer
	caPool        *x509.CertPool
}

func NewProxyDialer(address, tlsServerName, fakeSNI, auth stringCb, caPool *x509.CertPool, nextDialer ContextDialer) *ProxyDialer {
	return &ProxyDialer{
		address:       address,
		tlsServerName: tlsServerName,
		fakeSNI:       fakeSNI,
		auth:          auth,
		next:          nextDialer,
		caPool:        caPool,
	}
}

func ProxyDialerFromURL(u *url.URL, next ContextDialer) (*ProxyDialer, error) {
	host := u.Hostname()
	port := u.Port()
	tlsServerName := ""
	var auth stringCb = nil

	switch strings.ToLower(u.Scheme) {
	case "http":
		if port == "" {
			port = "80"
		}
	case "https":
		if port == "" {
			port = "443"
		}
		tlsServerName = host
	default:
		return nil, errors.New("unsupported proxy type")
	}

	address := net.JoinHostPort(host, port)

	if u.User != nil {
		username := u.User.Username()
		password, _ := u.User.Password()
		auth = WrapStringToCb(BasicAuthHeader(username, password))
	}
	return NewProxyDialer(
		WrapStringToCb(address),
		WrapStringToCb(tlsServerName),
		WrapStringToCb(tlsServerName),
		auth,
		nil,
		next), nil
}

func (d *ProxyDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, errors.New("bad network specified for DialContext: only tcp is supported")
	}

	uAddress, err := d.address()
	if err != nil {
		return nil, err
	}
	conn, err := d.next.DialContext(ctx, "tcp", uAddress)
	if err != nil {
		return nil, err
	}

	uTLSServerName, err := d.tlsServerName()
	if err != nil {
		return nil, err
	}
	fakeSNI, err := d.fakeSNI()
	if err != nil {
		return nil, err
	}
	if uTLSServerName != "" {
		// Custom cert verification logic:
		// DO NOT send SNI extension of TLS ClientHello
		// DO peer certificate verification against specified servername
		conn = tls.Client(conn, &tls.Config{
			ServerName:         fakeSNI,
			InsecureSkipVerify: true,
			VerifyConnection: func(cs tls.ConnectionState) error {
				opts := x509.VerifyOptions{
					DNSName:       uTLSServerName,
					Intermediates: x509.NewCertPool(),
					Roots:         d.caPool,
				}
				for _, cert := range cs.PeerCertificates[1:] {
					opts.Intermediates.AddCert(cert)
				}
				_, err := cs.PeerCertificates[0].Verify(opts)
				return err
			},
		})
	}

	req := &http.Request{
		Method:     PROXY_CONNECT_METHOD,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		RequestURI: address,
		Host:       address,
		Header: http.Header{
			PROXY_HOST_HEADER: []string{address},
		},
	}

	if d.auth != nil {
		auth, err := d.auth()
		if err != nil {
			return nil, err
		}
		req.Header.Set(PROXY_AUTHORIZATION_HEADER, auth)
	}

	rawreq, err := httputil.DumpRequest(req, false)
	if err != nil {
		return nil, err
	}

	_, err = conn.Write(rawreq)
	if err != nil {
		return nil, err
	}

	proxyResp, err := readResponse(conn, req)
	if err != nil {
		return nil, err
	}

	if proxyResp.StatusCode != http.StatusOK {
		return nil, errors.New(fmt.Sprintf("bad response from upstream proxy server: %s", proxyResp.Status))
	}

	return conn, nil
}

func (d *ProxyDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

func (d *ProxyDialer) Address() (string, error) {
	return d.address()
}

func readResponse(r io.Reader, req *http.Request) (*http.Response, error) {
	endOfResponse := []byte("\r\n\r\n")
	buf := &bytes.Buffer{}
	b := make([]byte, 1)
	for {
		n, err := r.Read(b)
		if n < 1 && err == nil {
			continue
		}

		buf.Write(b)
		sl := buf.Bytes()
		if len(sl) < len(endOfResponse) {
			continue
		}

		if bytes.Equal(sl[len(sl)-4:], endOfResponse) {
			break
		}

		if err != nil {
			return nil, err
		}
	}
	return http.ReadResponse(bufio.NewReader(buf), req)
}

func BasicAuthHeader(login, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString(
		[]byte(login+":"+password))
}

func WrapStringToCb(s string) func() (string, error) {
	return func() (string, error) {
		return s, nil
	}
}
