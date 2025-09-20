package dialer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
)

type ServerSelection int

const (
	_ = iota
	ServerSelectionFirst
	ServerSelectionRandom
	ServerSelectionFastest
)

func (ss ServerSelection) String() string {
	switch ss {
	case ServerSelectionFirst:
		return "first"
	case ServerSelectionRandom:
		return "random"
	case ServerSelectionFastest:
		return "fastest"
	default:
		return fmt.Sprintf("ServerSelection(%d)", int(ss))
	}
}

func ParseServerSelection(s string) (ServerSelection, error) {
	switch strings.ToLower(s) {
	case "first":
		return ServerSelectionFirst, nil
	case "random":
		return ServerSelectionRandom, nil
	case "fastest":
		return ServerSelectionFastest, nil
	}
	return 0, errors.New("unknown server selection strategy")
}

type SelectionFunc = func(ctx context.Context, dialers []ContextDialer) (ContextDialer, error)

func SelectFirst(_ context.Context, dialers []ContextDialer) (ContextDialer, error) {
	if len(dialers) == 0 {
		return nil, errors.New("empty dialers list")
	}
	return dialers[0], nil
}

func SelectRandom(_ context.Context, dialers []ContextDialer) (ContextDialer, error) {
	if len(dialers) == 0 {
		return nil, errors.New("empty dialers list")
	}
	return dialers[rand.IntN(len(dialers))], nil
}

func probeDialer(ctx context.Context, dialer ContextDialer, url string, dlLimit int64, tlsClientConfig *tls.Config) error {
	httpClient := http.Client{
		Transport: &http.Transport{
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DialContext:           dialer.DialContext,
			TLSClientConfig:       tlsClientConfig,
			ForceAttemptHTTP2:     true,
		},
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status code %d for URL %q", resp.StatusCode, url)
	}
	var rd io.Reader = resp.Body
	if dlLimit > 0 {
		rd = io.LimitReader(rd, dlLimit)
	}
	_, err = io.Copy(io.Discard, rd)
	return err
}

func NewFastestServerSelectionFunc(url string, dlLimit int64, tlsClientConfig *tls.Config) SelectionFunc {
	return func(ctx context.Context, dialers []ContextDialer) (ContextDialer, error) {
		var resErr error
		masterNotInterested := make(chan struct{})
		defer close(masterNotInterested)
		errors := make(chan error)
		success := make(chan ContextDialer)
		for _, dialer := range dialers {
			go func(dialer ContextDialer) {
				err := probeDialer(ctx, dialer, url, dlLimit, tlsClientConfig)
				if err == nil {
					select {
					case success <- dialer:
					case <-masterNotInterested:
					}
				} else {
					select {
					case errors <- err:
					case <-masterNotInterested:
					}
				}
			}(dialer)
		}
		for _ = range dialers {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case d := <-success:
				return d, nil
			case err := <-errors:
				resErr = multierror.Append(resErr, err)
			}
		}
		return nil, resErr
	}
}
