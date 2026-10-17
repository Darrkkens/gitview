package httpcol

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// ErrNotLoopback is returned when a connection to a non-loopback address
// is attempted.
var ErrNotLoopback = errors.New("GitView only connects to loopback addresses")

// LoopbackClient is the HTTP client GitView uses to talk to a monitored
// process: it connects only to loopback addresses (checked on the resolved
// address, at connect time), never follows redirects and ignores proxy
// settings.
func LoopbackClient(timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: timeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("%w (refused %s)", ErrNotLoopback, address)
		}
		return nil
	}}
	return &http.Client{
		Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return d.DialContext(ctx, network, addr)
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
