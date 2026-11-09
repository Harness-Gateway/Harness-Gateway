package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalhostUsesNumericLoopback(t *testing.T) {
	for _, address := range []string{"localhost:0", "LOCALHOST:8080", "127.0.0.2:99", "[::1]:80"} {
		canonical, err := numericLoopbackAddress(address)
		if err != nil {
			t.Fatal(err)
		}
		host, _, err := net.SplitHostPort(canonical)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			t.Fatalf("address can reach a resolver: %q -> %q (%v)", address, canonical, err)
		}
	}
	// A hostile DNS response cannot affect either binding or diagnostic dialing.
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, fmt.Errorf("resolver must not be used for accepted loopback addresses")
	}}
	t.Cleanup(func() { net.DefaultResolver = previous })
	listener, err := listenLoopback("localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	if !listener.Addr().(*net.TCPAddr).IP.IsLoopback() {
		t.Fatalf("non-loopback listener: %v", listener.Addr())
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Host, "localhost") {
			t.Error("diagnostics retained a resolvable hostname")
		}
		if r.URL.Path == "/health" {
			fmt.Fprint(w, `{"status":"ok"}`)
		} else {
			fmt.Fprint(w, `{"object":"list","data":[{"id":"codex"}]}`)
		}
	}))
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	for _, check := range doctorHTTPChecks(t.Context(), "http://localhost:"+port, "") {
		if check.err != nil {
			t.Fatalf("diagnostics failed for pinned localhost: %s: %v", check.name, check.err)
		}
	}
}
