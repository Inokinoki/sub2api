package cursor

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDialProxyConnRejectsUnsupportedScheme(t *testing.T) {
	_, err := dialProxyConn(context.Background(), testDialer(), "ftp://proxy.local:21", "target.local:443")
	require.ErrorContains(t, err, "unsupported proxy scheme")
}

func TestDialProxyConnInvalidURL(t *testing.T) {
	_, err := dialProxyConn(context.Background(), testDialer(), "http://[::1", "target.local:443")
	require.Error(t, err)
}

// TestDialHTTPConnectTunnelsThroughProxy spins up a minimal CONNECT proxy and
// verifies the tunnel is established (auth header present, 200 reply) and the
// returned conn is usable for application traffic.
func TestDialHTTPConnectTunnelsThroughProxy(t *testing.T) {
	requestSeen := make(chan string, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read the CONNECT request headers.
		buf := make([]byte, 0, 512)
		tmp := make([]byte, 256)
		for !strings.Contains(string(buf), "\r\n\r\n") {
			n, err := conn.Read(tmp)
			if err != nil {
				return
			}
			buf = append(buf, tmp[:n]...)
		}
		select {
		case requestSeen <- string(buf):
		default:
		}
		if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			return
		}
		_, _ = io.Copy(conn, conn) // echo anything sent through the tunnel
	}()
	t.Cleanup(func() { _ = ln.Close() })

	conn, err := dialProxyConn(context.Background(), testDialer(), "http://user:pass@"+ln.Addr().String(), "target.local:443")
	require.NoError(t, err)
	defer conn.Close()

	request := <-requestSeen
	require.Contains(t, request, "CONNECT target.local:443")
	require.Contains(t, request, "Proxy-Authorization: Basic ")

	_, err = conn.Write([]byte("ping"))
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	echo := make([]byte, 4)
	_, err = io.ReadFull(conn, echo)
	require.NoError(t, err)
	require.Equal(t, "ping", string(echo))
}

func TestStreamingHTTPClientReusesTransportPerProxy(t *testing.T) {
	first, err := StreamingHTTPClient("")
	require.NoError(t, err)
	second, err := StreamingHTTPClient("")
	require.NoError(t, err)
	require.Same(t, first.Transport, second.Transport)
	require.Zero(t, first.Timeout, "streaming client must not set an overall timeout")
}

func TestUnaryHTTPClientSetsOverallTimeout(t *testing.T) {
	client, err := UnaryHTTPClient("")
	require.NoError(t, err)
	require.Equal(t, cursorUnaryTimeout, client.Timeout)
}

func testDialer() *net.Dialer {
	return &net.Dialer{Timeout: 5 * time.Second}
}
