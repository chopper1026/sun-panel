package siteFavicon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var testPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

func TestGetOneFaviconURLAddsHTTPForHostPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><link rel="icon" href="/favicon.png"></head></html>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	input := strings.TrimPrefix(server.URL, "http://")
	iconURL, err := GetOneFaviconURL(input)
	if err != nil {
		t.Fatalf("expected icon URL, got error: %v", err)
	}

	want := server.URL + "/favicon.png"
	if iconURL != want {
		t.Fatalf("icon URL mismatch\nwant: %s\n got: %s", want, iconURL)
	}
}

func TestGetOneFaviconURLFallsBackToFaviconICO(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><title>No explicit favicon</title></head></html>`))
		case "/favicon.ico":
			w.Header().Set("Content-Type", "image/x-icon")
			_, _ = w.Write([]byte("ico"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	iconURL, err := GetOneFaviconURL(server.URL)
	if err != nil {
		t.Fatalf("expected fallback icon URL, got error: %v", err)
	}

	want := server.URL + "/favicon.ico"
	if iconURL != want {
		t.Fatalf("icon URL mismatch\nwant: %s\n got: %s", want, iconURL)
	}
}

func TestGetOneFaviconURLResolvesRelativeIconAgainstPageURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/login" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><link rel="icon" href="../assets/favicon.png"></head></html>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	iconURL, err := GetOneFaviconURL(server.URL + "/app/login")
	if err != nil {
		t.Fatalf("expected relative icon URL, got error: %v", err)
	}

	want := server.URL + "/assets/favicon.png"
	if iconURL != want {
		t.Fatalf("icon URL mismatch\nwant: %s\n got: %s", want, iconURL)
	}
}

func TestDownloadImageUsesGETWhenHEADIsUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG)
	}))
	defer server.Close()

	file, err := DownloadImage(server.URL+"/favicon.png", t.TempDir(), int64(len(testPNG)+16))
	if err != nil {
		t.Fatalf("expected download success without HEAD support, got error: %v", err)
	}
	defer file.Close()

	got, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(got, testPNG) {
		t.Fatalf("downloaded bytes mismatch")
	}
}

func TestDownloadImageEnforcesActualReadLimit(t *testing.T) {
	payload := append(testPNG, []byte("too-large")...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	savePath := t.TempDir()
	_, err := DownloadImage(server.URL+"/favicon.png", savePath, int64(len(testPNG)))
	if err == nil {
		t.Fatal("expected download to fail when actual bytes exceed max size")
	}

	matches, globErr := filepath.Glob(filepath.Join(savePath, "*"))
	if globErr != nil {
		t.Fatalf("glob save path: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("expected oversized partial file to be removed, found %v", matches)
	}
}

func TestValidateURLSafetyAllowsPrivateLANByDefault(t *testing.T) {
	options := DefaultOptions()
	parsed, err := ValidateURLSafety("192.168.1.20:5000", options)
	if err != nil {
		t.Fatalf("expected private LAN address to be allowed by default, got error: %v", err)
	}
	if parsed.String() != "http://192.168.1.20:5000" {
		t.Fatalf("normalized URL mismatch: %s", parsed.String())
	}
}

func TestValidateURLSafetyRejectsLocalhostWithDockerHint(t *testing.T) {
	_, err := ValidateURLSafety("http://localhost:3000", DefaultOptions())
	if err == nil {
		t.Fatal("expected localhost to be rejected")
	}
	if !strings.Contains(err.Error(), "Docker 容器中的 localhost") {
		t.Fatalf("expected Docker localhost hint, got: %v", err)
	}
}

func TestValidateURLSafetyDenyHostWinsOverAllowCIDR(t *testing.T) {
	options := DefaultOptions()
	options.DenyHosts = []string{"127.0.0.1"}
	options.AllowCIDRs = []string{"127.0.0.0/8"}

	_, err := ValidateURLSafety("http://127.0.0.1:3000", options)
	if err == nil {
		t.Fatal("expected denied IP to be rejected even when allowed by CIDR")
	}
	if !strings.Contains(err.Error(), "Docker 容器中的 localhost") {
		t.Fatalf("expected denied localhost error, got: %v", err)
	}
}

func TestDownloadImageWithOptionsRejectsRedirectToDeniedHost(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start.png" {
			port := server.Listener.Addr().(*net.TCPAddr).Port
			http.Redirect(w, r, "http://localhost:"+strconv.Itoa(port)+"/favicon.png", http.StatusFound)
			return
		}

		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG)
	}))
	defer server.Close()

	options := DefaultOptions()
	options.AllowCIDRs = []string{"127.0.0.0/8"}
	options.DenyHosts = []string{"localhost"}

	_, err := DownloadImageWithOptions(server.URL+"/start.png", t.TempDir(), options)
	if err == nil {
		t.Fatal("expected redirect to denied host to be rejected")
	}
	if !strings.Contains(err.Error(), "localhost") {
		t.Fatalf("expected denied localhost error, got: %v", err)
	}
}

func TestHashFileReturnsSHA256AndSize(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "favicon.png")
	if err := os.WriteFile(filePath, testPNG, 0600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	hash, size, err := HashFile(filePath)
	if err != nil {
		t.Fatalf("hash file: %v", err)
	}

	sum := sha256.Sum256(testPNG)
	if hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash mismatch\nwant: %s\n got: %s", hex.EncodeToString(sum[:]), hash)
	}
	if size != int64(len(testPNG)) {
		t.Fatalf("size mismatch: %d", size)
	}
}

func TestGetOneFaviconURLUsesManifestIcon(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><link rel="manifest" href="/manifest.webmanifest"></head></html>`))
		case "/manifest.webmanifest":
			w.Header().Set("Content-Type", "application/manifest+json")
			_, _ = w.Write([]byte(`{
				"icons": [
					{"src": "/icon-maskable.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"},
					{"src": "/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any"}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	iconURL, err := GetOneFaviconURL(server.URL)
	if err != nil {
		t.Fatalf("expected manifest icon URL, got error: %v", err)
	}

	want := server.URL + "/icon-192.png"
	if iconURL != want {
		t.Fatalf("icon URL mismatch\nwant: %s\n got: %s", want, iconURL)
	}
}

func TestGetOneFaviconURLPrefersAppleTouchIconOverICO(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head>
			<link rel="icon" href="/favicon.ico" sizes="16x16" type="image/x-icon">
			<link rel="apple-touch-icon" href="/apple-touch-icon.png" sizes="180x180" type="image/png">
		</head></html>`))
	}))
	defer server.Close()

	iconURL, err := GetOneFaviconURL(server.URL)
	if err != nil {
		t.Fatalf("expected icon URL, got error: %v", err)
	}

	want := server.URL + "/apple-touch-icon.png"
	if iconURL != want {
		t.Fatalf("icon URL mismatch\nwant: %s\n got: %s", want, iconURL)
	}
}

func TestGetOneFaviconURLPrefersSVGIcon(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head>
			<link rel="icon" href="/icon-192.png" sizes="192x192" type="image/png">
			<link rel="icon" href="/icon.svg" type="image/svg+xml">
		</head></html>`))
	}))
	defer server.Close()

	iconURL, err := GetOneFaviconURL(server.URL)
	if err != nil {
		t.Fatalf("expected SVG icon URL, got error: %v", err)
	}

	want := server.URL + "/icon.svg"
	if iconURL != want {
		t.Fatalf("icon URL mismatch\nwant: %s\n got: %s", want, iconURL)
	}
}

func TestGetFaviconURLsWithOptionsUsesConfiguredHTTPProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("target should be reached through proxy, got direct request for %s", r.URL.String())
	}))
	defer target.Close()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != target.URL {
			t.Fatalf("proxy received URL mismatch\nwant: %s\n got: %s", target.URL, r.URL.String())
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><link rel="icon" href="/favicon.png"></head></html>`))
	}))
	defer proxy.Close()

	options := DefaultOptions()
	options.ProxyURL = proxy.URL
	options.ProxyFromEnv = false
	options.NoProxy = nil

	icons, err := GetFaviconURLsWithOptions(target.URL, options)
	if err != nil {
		t.Fatalf("expected favicon discovery through proxy, got error: %v", err)
	}
	if len(icons) == 0 || icons[0] != target.URL+"/favicon.png" {
		t.Fatalf("unexpected icon candidates: %#v", icons)
	}
}

func TestGetFaviconURLsWithOptionsBypassesProxyForNoProxyCIDR(t *testing.T) {
	proxyHit := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHit = true
		http.Error(w, "proxy should be bypassed", http.StatusBadGateway)
	}))
	defer proxy.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><link rel="icon" href="/favicon.png"></head></html>`))
	}))
	defer target.Close()

	options := DefaultOptions()
	options.ProxyURL = proxy.URL
	options.ProxyFromEnv = false
	options.NoProxy = []string{"127.0.0.0/8"}
	options.AllowCIDRs = []string{"127.0.0.0/8"}
	options.DenyHosts = []string{}

	icons, err := GetFaviconURLsWithOptions(target.URL, options)
	if err != nil {
		t.Fatalf("expected direct favicon discovery for no-proxy target, got error: %v", err)
	}
	if proxyHit {
		t.Fatal("expected proxy to be bypassed")
	}
	if len(icons) == 0 || icons[0] != target.URL+"/favicon.png" {
		t.Fatalf("unexpected icon candidates: %#v", icons)
	}
}

func TestGetFaviconURLsWithOptionsRejectsInvalidProxyURL(t *testing.T) {
	options := DefaultOptions()
	options.ProxyURL = "socks5://127.0.0.1:7890"

	_, err := GetFaviconURLsWithOptions("https://example.com", options)
	if err == nil {
		t.Fatal("expected invalid proxy URL error")
	}
	if !strings.Contains(err.Error(), "仅支持 HTTP/HTTPS 代理") {
		t.Fatalf("unexpected error: %v", err)
	}
}
