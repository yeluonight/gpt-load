package httpclient

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestStripSensitiveOnCrossHostRedirect asserts that the custom-named x-api-key
// credential header set by a proxy channel's ModifyRequest is NOT replayed to a
// different host when the operator-configured upstream issues a cross-host
// redirect. Regression test for the upstream-key leak (CWE-200 / CWE-522).
func TestStripSensitiveOnCrossHostRedirect(t *testing.T) {
	var gotAPIKey, gotAuthorization string

	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer attacker.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://attacker.local/v1/messages", http.StatusFound)
	}))
	defer upstream.Close()

	attackerAddr := strings.TrimPrefix(attacker.URL, "http://")
	upstreamAddr := strings.TrimPrefix(upstream.URL, "http://")

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			switch addr {
			case "victim.local:80":
				addr = upstreamAddr
			case "attacker.local:80":
				addr = attackerAddr
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}

	client := &http.Client{
		Transport:     transport,
		CheckRedirect: stripSensitiveOnCrossHostRedirect,
	}

	req, _ := http.NewRequest(http.MethodPost, "http://victim.local/v1/messages", nil)
	req.Header.Set("x-api-key", "sk-secret-upstream-key")
	req.Header.Set("Authorization", "Bearer secret-bearer")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if gotAPIKey != "" {
		t.Errorf("x-api-key leaked to cross-host redirect target: %q", gotAPIKey)
	}
	if gotAuthorization != "" {
		t.Errorf("Authorization leaked to cross-host redirect target: %q", gotAuthorization)
	}
}

func TestExplicitInvalidProxyDoesNotFallbackToEnvironment(t *testing.T) {
	proxyHit := false
	envProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHit = true
		w.WriteHeader(http.StatusTeapot)
	}))
	defer envProxy.Close()
	t.Setenv("HTTP_PROXY", envProxy.URL)
	t.Setenv("HTTPS_PROXY", envProxy.URL)
	t.Setenv("NO_PROXY", "")

	manager := NewHTTPClientManager()
	client := manager.GetClient(&Config{
		ConnectTimeout:        100000000,
		RequestTimeout:        100000000,
		IdleConnTimeout:       100000000,
		MaxIdleConns:          1,
		MaxIdleConnsPerHost:   1,
		ResponseHeaderTimeout: 100000000,
		ProxyURL:              "://bad-proxy-url",
	})

	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	resp, err := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected invalid explicit proxy to fail")
	}
	if proxyHit {
		t.Fatal("request fell back to environment proxy despite explicit proxy config")
	}
}

func TestExplicitProxyReceivesHTTPSConnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen proxy: %v", err)
	}
	defer listener.Close()

	firstLineCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		firstLineCh <- strings.TrimSpace(line)
	}()

	manager := NewHTTPClientManager()
	client := manager.GetClient(&Config{
		ConnectTimeout:        time.Second,
		RequestTimeout:        time.Second,
		IdleConnTimeout:       time.Second,
		MaxIdleConns:          1,
		MaxIdleConnsPerHost:   1,
		ResponseHeaderTimeout: time.Second,
		ProxyURL:              "http://" + listener.Addr().String(),
	})

	req, _ := http.NewRequest(http.MethodGet, "https://example.com/v1beta/models", nil)
	resp, _ := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}

	select {
	case line := <-firstLineCh:
		if line != "CONNECT example.com:443 HTTP/1.1" {
			t.Fatalf("proxy received first line %q, want HTTPS CONNECT", line)
		}
	case err := <-errCh:
		t.Fatalf("proxy failed before receiving request: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not receive an HTTPS CONNECT request")
	}
}

// TestSensitiveHeadersPreservedSameHost asserts the policy does NOT strip the
// credential header on a same-host redirect (legitimate behavior must survive).
func TestSensitiveHeadersPreservedSameHost(t *testing.T) {
	var gotAPIKey string
	var hops int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			hops++
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		gotAPIKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{CheckRedirect: stripSensitiveOnCrossHostRedirect}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/redirect", nil)
	req.Header.Set("x-api-key", "sk-secret-upstream-key")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if hops == 0 {
		t.Fatal("expected a same-host redirect hop")
	}
	if gotAPIKey != "sk-secret-upstream-key" {
		t.Errorf("x-api-key was incorrectly stripped on same-host redirect: %q", gotAPIKey)
	}
}
