# Favicon Network Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an administrator-only Network Settings app below Account Management so favicon fetching can use a configurable HTTP/HTTPS proxy outside Docker deployments.

**Architecture:** Favicon HTTP behavior remains isolated inside `service/lib/siteFavicon`. The panel API stores global settings in `system_settings` under `favicon_network`, resolves them over `conf.ini` defaults, and the frontend exposes a compact Naive UI settings form in the system app launcher.

**Tech Stack:** Go, Gin, GORM, existing `systemSetting` cache, Vue 3, Naive UI, TypeScript, existing pnpm/Vite toolchain.

---

## File Structure

- Modify `service/lib/siteFavicon/favico.go`: add proxy/no-proxy fields to `Options`, configure the HTTP client transport, and add proxy matching helpers.
- Modify `service/lib/siteFavicon/favico_test.go`: add proxy/no-proxy tests around `GetFaviconURLsWithOptions`.
- Modify `service/lib/cmn/systemSetting/systemSetting.go`: add `FAVICON_NETWORK` key and `FaviconNetworkSetting` struct.
- Create `service/api/api_v1/panel/networkSetting.go`: add admin-only handlers for get/save/test and shared effective config resolution.
- Modify `service/api/api_v1/panel/itemIcon.go`: make favicon fetching use the shared effective config resolver.
- Modify `service/api/api_v1/panel/A_ENTER.go`: add `NetworkSetting` to `ApiPanel`.
- Create `service/router/panel/networkSetting.go`: register authenticated admin routes.
- Modify `service/router/panel/A_ENTER.go`: initialize the new router.
- Modify `service/initialize/config/config.go`, `service/conf/conf.ini`, `service/conf/conf.example.ini`, and `service/assets/conf.example.ini`: add deployment defaults.
- Create `src/api/panel/networkSetting.ts`: frontend API wrapper.
- Create `src/typings/networkSetting.d.ts`: frontend request/response types.
- Create `src/components/apps/NetworkSettings/index.vue`: settings UI.
- Modify `src/views/home/components/AppStarter/index.vue`: add Network Settings below Account Management for administrators.
- Modify `src/components/apps/index.ts`: export the new app component.
- Modify `src/locales/zh-CN.json` and `src/locales/en-US.json`: add labels/messages.

## Task 1: Backend Proxy Support in siteFavicon

**Files:**
- Modify: `service/lib/siteFavicon/favico.go`
- Test: `service/lib/siteFavicon/favico_test.go`

- [ ] **Step 1: Run impact analysis before modifying symbols**

Run:

```bash
gitnexus_impact({target: "Options", direction: "upstream", repo: "sun-panel"})
gitnexus_impact({target: "DefaultOptions", direction: "upstream", repo: "sun-panel"})
gitnexus_impact({target: "newHTTPClient", direction: "upstream", repo: "sun-panel"})
```

Expected: review direct callers and risk. If HIGH or CRITICAL appears, report it before editing.

- [ ] **Step 2: Write failing proxy tests**

Append these tests to `service/lib/siteFavicon/favico_test.go`:

```go
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
```

- [ ] **Step 3: Run tests and verify they fail**

Run:

```bash
cd service && GOPROXY=${GOPROXY:-https://goproxy.cn,direct} go test ./lib/siteFavicon
```

Expected: FAIL because `Options.ProxyURL`, `Options.ProxyFromEnv`, and `Options.NoProxy` do not exist.

- [ ] **Step 4: Implement proxy fields and client transport**

In `service/lib/siteFavicon/favico.go`, extend `Options`:

```go
type Options struct {
	Timeout             time.Duration
	MaxDownloadBytes    int64
	MaxRedirects        int
	AllowPrivateNetwork bool
	AllowCIDRs          []string
	DenyHosts           []string
	SkipSafetyCheck     bool
	ProxyURL            string
	ProxyFromEnv        bool
	NoProxy             []string
}
```

Extend `DefaultOptions()`:

```go
return Options{
	Timeout:             defaultTimeout,
	MaxDownloadBytes:    1024 * 1024,
	MaxRedirects:        3,
	AllowPrivateNetwork: true,
	AllowCIDRs: []string{
		"192.168.0.0/16",
		"10.0.0.0/8",
		"172.16.0.0/12",
	},
	DenyHosts: []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"169.254.169.254",
	},
	ProxyFromEnv: true,
	NoProxy: []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"192.168.0.0/16",
		"10.0.0.0/8",
		"172.16.0.0/12",
	},
}
```

Replace `newHTTPClient` with a transport-aware version:

```go
func newHTTPClient(options Options) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxyFunc(options)

	return &http.Client{
		Timeout:   options.Timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if options.MaxRedirects > 0 && len(via) >= options.MaxRedirects {
				return errors.New("重定向次数过多")
			}
			if options.SkipSafetyCheck {
				return nil
			}
			return validateParsedURLSafety(req.URL, options)
		},
	}
}
```

Add helpers below `newHTTPClient`:

```go
func proxyFunc(options Options) func(*http.Request) (*url.URL, error) {
	var configuredProxy *url.URL
	if strings.TrimSpace(options.ProxyURL) != "" {
		parsed, err := parseProxyURL(options.ProxyURL)
		if err != nil {
			return func(*http.Request) (*url.URL, error) {
				return nil, err
			}
		}
		configuredProxy = parsed
	}

	return func(req *http.Request) (*url.URL, error) {
		if req == nil || req.URL == nil {
			return nil, nil
		}
		if shouldBypassProxy(req.URL, options.NoProxy) {
			return nil, nil
		}
		if configuredProxy != nil {
			return configuredProxy, nil
		}
		if options.ProxyFromEnv {
			return http.ProxyFromEnvironment(req)
		}
		return nil, nil
	}
}

func parseProxyURL(rawProxyURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawProxyURL))
	if err != nil {
		return nil, fmt.Errorf("代理地址格式无效: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("仅支持 HTTP/HTTPS 代理")
	}
	if parsed.Host == "" {
		return nil, errors.New("代理地址缺少主机名")
	}
	return parsed, nil
}

func shouldBypassProxy(targetURL *url.URL, rules []string) bool {
	host := strings.TrimSpace(targetURL.Hostname())
	if host == "" {
		return false
	}
	hostLower := strings.ToLower(host)
	for _, rawRule := range rules {
		rule := strings.ToLower(strings.TrimSpace(rawRule))
		if rule == "" {
			continue
		}
		if rule == "*" {
			return true
		}
		if _, network, err := net.ParseCIDR(rule); err == nil {
			if ip := net.ParseIP(host); ip != nil && network.Contains(ip) {
				return true
			}
			continue
		}
		if ip := net.ParseIP(rule); ip != nil {
			if hostIP := net.ParseIP(host); hostIP != nil && hostIP.Equal(ip) {
				return true
			}
			continue
		}
		if hostLower == rule || strings.HasSuffix(hostLower, "."+strings.TrimPrefix(rule, ".")) {
			return true
		}
	}
	return false
}
```

Extend `normalizeOptions`:

```go
if options.NoProxy == nil {
	options.NoProxy = defaults.NoProxy
}
```

- [ ] **Step 5: Run site favicon tests**

Run:

```bash
cd service && GOPROXY=${GOPROXY:-https://goproxy.cn,direct} go test ./lib/siteFavicon
```

Expected: PASS.

- [ ] **Step 6: Commit backend proxy support**

Run:

```bash
git add service/lib/siteFavicon/favico.go service/lib/siteFavicon/favico_test.go
git commit -m "feat: support favicon proxy options"
```

## Task 2: System Setting Storage and Panel API

**Files:**
- Modify: `service/lib/cmn/systemSetting/systemSetting.go`
- Create: `service/api/api_v1/panel/networkSetting.go`
- Modify: `service/api/api_v1/panel/itemIcon.go`
- Modify: `service/api/api_v1/panel/A_ENTER.go`
- Create: `service/router/panel/networkSetting.go`
- Modify: `service/router/panel/A_ENTER.go`
- Modify: `service/initialize/config/config.go`
- Modify: `service/conf/conf.ini`
- Modify: `service/conf/conf.example.ini`
- Modify: `service/assets/conf.example.ini`

- [ ] **Step 1: Run impact analysis before modifying symbols**

Run:

```bash
gitnexus_impact({target: "SystemSettingCache", direction: "upstream", repo: "sun-panel"})
gitnexus_impact({target: "GetSiteFavicon", direction: "upstream", repo: "sun-panel"})
gitnexus_impact({target: "ApiPanel", direction: "upstream", repo: "sun-panel"})
gitnexus_impact({target: "Init", file_path: "service/router/panel/A_ENTER.go", direction: "upstream", repo: "sun-panel"})
gitnexus_impact({target: "getDefaultConfig", direction: "upstream", repo: "sun-panel"})
```

Expected: review risk. If HIGH or CRITICAL appears, report it before editing.

- [ ] **Step 2: Add system setting type**

In `service/lib/cmn/systemSetting/systemSetting.go`, add:

```go
const (
	FAVICON_NETWORK = "favicon_network"
)

type FaviconNetworkSetting struct {
	ProxyURL       string `json:"proxyUrl"`
	ProxyFromEnv   bool   `json:"proxyFromEnv"`
	NoProxy        string `json:"noProxy"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}
```

Keep the new constant in the existing const block rather than creating a second const block.

- [ ] **Step 3: Create panel API handlers**

Create `service/api/api_v1/panel/networkSetting.go`:

```go
package panel

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sun-panel/api/api_v1/common/apiReturn"
	"sun-panel/global"
	"sun-panel/lib/cmn/systemSetting"
	"sun-panel/lib/siteFavicon"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

type NetworkSetting struct{}

type faviconNetworkTestReq struct {
	URL     string                               `json:"url" binding:"required"`
	Setting systemSetting.FaviconNetworkSetting `json:"setting"`
}

type faviconNetworkTestResp struct {
	IconURLs []string `json:"iconUrls"`
	Proxy    string   `json:"proxy"`
}

func (a *NetworkSetting) GetFavicon(c *gin.Context) {
	setting := getFaviconNetworkSettingFromConfig()
	apiReturn.SuccessData(c, setting)
}

func (a *NetworkSetting) SetFavicon(c *gin.Context) {
	req := systemSetting.FaviconNetworkSetting{}
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}
	normalized, err := normalizeFaviconNetworkSetting(req)
	if err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}
	if err := global.SystemSetting.Set(systemSetting.FAVICON_NETWORK, normalized); err != nil {
		apiReturn.ErrorDatabase(c, err.Error())
		return
	}
	apiReturn.SuccessData(c, normalized)
}

func (a *NetworkSetting) TestFavicon(c *gin.Context) {
	req := faviconNetworkTestReq{}
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}
	setting, err := normalizeFaviconNetworkSetting(req.Setting)
	if err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}
	options := getFaviconOptionsFromSetting(setting)
	iconURLs, err := siteFavicon.GetFaviconURLsWithOptions(req.URL, options)
	if err != nil {
		apiReturn.Error(c, "图标获取失败："+err.Error())
		return
	}
	apiReturn.SuccessData(c, faviconNetworkTestResp{
		IconURLs: iconURLs,
		Proxy:    safeProxyLabel(setting),
	})
}
```

Then implement the helper functions in the same file:

```go
func getFaviconNetworkSettingFromConfig() systemSetting.FaviconNetworkSetting {
	setting := systemSetting.FaviconNetworkSetting{
		ProxyURL:       strings.TrimSpace(global.Config.GetValueStringOrDefault("favicon", "proxy_url")),
		ProxyFromEnv:   true,
		NoProxy:        global.Config.GetValueStringOrDefault("favicon", "no_proxy"),
		TimeoutSeconds: 15,
	}
	if timeoutSeconds, err := strconv.Atoi(strings.TrimSpace(global.Config.GetValueStringOrDefault("favicon", "timeout_seconds"))); err == nil && timeoutSeconds > 0 {
		setting.TimeoutSeconds = timeoutSeconds
	}
	if proxyFromEnv, err := strconv.ParseBool(strings.TrimSpace(global.Config.GetValueStringOrDefault("favicon", "proxy_from_env"))); err == nil {
		setting.ProxyFromEnv = proxyFromEnv
	}
	if global.SystemSetting != nil {
		dbSetting := systemSetting.FaviconNetworkSetting{}
		if err := global.SystemSetting.GetValueByInterface(systemSetting.FAVICON_NETWORK, &dbSetting); err == nil {
			setting = mergeFaviconNetworkSetting(setting, dbSetting)
		}
	}
	normalized, err := normalizeFaviconNetworkSetting(setting)
	if err != nil {
		return systemSetting.FaviconNetworkSetting{
			ProxyFromEnv:   true,
			NoProxy:        "localhost,127.0.0.1,::1,192.168.0.0/16,10.0.0.0/8,172.16.0.0/12",
			TimeoutSeconds: 15,
		}
	}
	return normalized
}

func mergeFaviconNetworkSetting(base, override systemSetting.FaviconNetworkSetting) systemSetting.FaviconNetworkSetting {
	base.ProxyURL = override.ProxyURL
	base.ProxyFromEnv = override.ProxyFromEnv
	if strings.TrimSpace(override.NoProxy) != "" {
		base.NoProxy = override.NoProxy
	}
	if override.TimeoutSeconds > 0 {
		base.TimeoutSeconds = override.TimeoutSeconds
	}
	return base
}

func normalizeFaviconNetworkSetting(setting systemSetting.FaviconNetworkSetting) (systemSetting.FaviconNetworkSetting, error) {
	setting.ProxyURL = strings.TrimSpace(setting.ProxyURL)
	setting.NoProxy = strings.TrimSpace(setting.NoProxy)
	if setting.NoProxy == "" {
		setting.NoProxy = "localhost,127.0.0.1,::1,192.168.0.0/16,10.0.0.0/8,172.16.0.0/12"
	}
	if setting.TimeoutSeconds <= 0 {
		setting.TimeoutSeconds = 15
	}
	if setting.TimeoutSeconds > 120 {
		setting.TimeoutSeconds = 120
	}
	if setting.ProxyURL != "" {
		parsed, err := url.Parse(setting.ProxyURL)
		if err != nil {
			return setting, fmt.Errorf("代理地址格式无效: %w", err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return setting, fmt.Errorf("仅支持 HTTP/HTTPS 代理")
		}
		if parsed.Host == "" {
			return setting, fmt.Errorf("代理地址缺少主机名")
		}
	}
	return setting, nil
}

func getFaviconOptionsFromSetting(setting systemSetting.FaviconNetworkSetting) siteFavicon.Options {
	options := siteFavicon.DefaultOptions()
	options.Timeout = time.Duration(setting.TimeoutSeconds) * time.Second
	options.ProxyURL = setting.ProxyURL
	options.ProxyFromEnv = setting.ProxyFromEnv
	options.NoProxy = splitConfigList(setting.NoProxy)
	if maxDownloadBytes, err := strconv.ParseInt(strings.TrimSpace(global.Config.GetValueStringOrDefault("favicon", "max_download_bytes")), 10, 64); err == nil && maxDownloadBytes > 0 {
		options.MaxDownloadBytes = maxDownloadBytes
	}
	if allowPrivateNetwork, err := strconv.ParseBool(strings.TrimSpace(global.Config.GetValueStringOrDefault("favicon", "allow_private_network"))); err == nil {
		options.AllowPrivateNetwork = allowPrivateNetwork
	}
	if allowCIDRs := splitConfigList(global.Config.GetValueStringOrDefault("favicon", "allow_cidrs")); len(allowCIDRs) > 0 {
		options.AllowCIDRs = allowCIDRs
	}
	if denyHosts := splitConfigList(global.Config.GetValueStringOrDefault("favicon", "deny_hosts")); len(denyHosts) > 0 {
		options.DenyHosts = denyHosts
	}
	return options
}

func safeProxyLabel(setting systemSetting.FaviconNetworkSetting) string {
	if setting.ProxyURL == "" {
		if setting.ProxyFromEnv {
			return "env"
		}
		return "direct"
	}
	parsed, err := url.Parse(setting.ProxyURL)
	if err != nil {
		return "configured"
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword("***", "***")
	}
	return parsed.String()
}
```

- [ ] **Step 4: Reuse shared resolver in GetSiteFavicon**

In `service/api/api_v1/panel/itemIcon.go`, replace the existing `getFaviconOptionsFromConfig` implementation with:

```go
func getFaviconOptionsFromConfig() siteFavicon.Options {
	return getFaviconOptionsFromSetting(getFaviconNetworkSettingFromConfig())
}
```

Keep `splitConfigList` in this file or move it to `networkSetting.go`; only one definition may remain in package `panel`.

- [ ] **Step 5: Register API group and routes**

In `service/api/api_v1/panel/A_ENTER.go`, add:

```go
NetworkSetting NetworkSetting
```

In `service/router/panel/networkSetting.go`, create:

```go
package panel

import (
	"sun-panel/api/api_v1"
	"sun-panel/api/api_v1/middleware"

	"github.com/gin-gonic/gin"
)

func InitNetworkSetting(router *gin.RouterGroup) {
	api := api_v1.ApiGroupApp.ApiPanel.NetworkSetting
	r := router.Group("", middleware.LoginInterceptor, middleware.AdminInterceptor)
	{
		r.POST("/panel/networkSetting/getFavicon", api.GetFavicon)
		r.POST("/panel/networkSetting/setFavicon", api.SetFavicon)
		r.POST("/panel/networkSetting/testFavicon", api.TestFavicon)
	}
}
```

In `service/router/panel/A_ENTER.go`, call:

```go
InitNetworkSetting(routerGroup)
```

- [ ] **Step 6: Add config defaults**

In `service/initialize/config/config.go`, extend the `favicon` defaults:

```go
"timeout_seconds":       "15",
"proxy_url":             "",
"proxy_from_env":        "true",
"no_proxy":              "localhost,127.0.0.1,::1,192.168.0.0/16,10.0.0.0/8,172.16.0.0/12",
```

In `service/conf/conf.ini`, `service/conf/conf.example.ini`, and `service/assets/conf.example.ini`, add under `[favicon]`:

```ini
# Optional HTTP/HTTPS proxy for fetching remote site pages and icons.
proxy_url=
# Use HTTP_PROXY/HTTPS_PROXY/NO_PROXY environment variables when proxy_url is empty.
proxy_from_env=true
# Hosts, IPs, and CIDR ranges that should bypass the favicon proxy.
no_proxy=localhost,127.0.0.1,::1,192.168.0.0/16,10.0.0.0/8,172.16.0.0/12
```

Also change the default `timeout_seconds` value in those files from `5` to `15`.

- [ ] **Step 7: Run backend tests**

Run:

```bash
cd service && GOPROXY=${GOPROXY:-https://goproxy.cn,direct} go test ./...
```

Expected: PASS.

- [ ] **Step 8: Commit API and config changes**

Run:

```bash
git add service/lib/cmn/systemSetting/systemSetting.go service/api/api_v1/panel/networkSetting.go service/api/api_v1/panel/itemIcon.go service/api/api_v1/panel/A_ENTER.go service/router/panel/networkSetting.go service/router/panel/A_ENTER.go service/initialize/config/config.go service/conf/conf.ini service/conf/conf.example.ini service/assets/conf.example.ini
git commit -m "feat: add favicon network settings api"
```

## Task 3: Frontend Network Settings App

**Files:**
- Create: `src/api/panel/networkSetting.ts`
- Create: `src/typings/networkSetting.d.ts`
- Create: `src/components/apps/NetworkSettings/index.vue`
- Modify: `src/views/home/components/AppStarter/index.vue`
- Modify: `src/components/apps/index.ts`
- Modify: `src/locales/zh-CN.json`
- Modify: `src/locales/en-US.json`

- [ ] **Step 1: Run impact analysis before modifying symbols**

Run:

```bash
gitnexus_impact({target: "handleClickApp", direction: "upstream", repo: "sun-panel"})
gitnexus_impact({target: "apps", file_path: "src/views/home/components/AppStarter/index.vue", direction: "upstream", repo: "sun-panel"})
```

Expected: review direct callers and risk. If HIGH or CRITICAL appears, report it before editing.

- [ ] **Step 2: Add frontend API wrapper**

Create `src/api/panel/networkSetting.ts`:

```ts
import { post } from '@/utils/request'

export function getFaviconNetwork<T>() {
  return post<T>({
    url: '/panel/networkSetting/getFavicon',
  })
}

export function setFaviconNetwork<T>(data: NetworkSetting.FaviconNetworkSetting) {
  return post<T>({
    url: '/panel/networkSetting/setFavicon',
    data,
  })
}

export function testFaviconNetwork<T>(data: NetworkSetting.FaviconNetworkTestRequest) {
  return post<T>({
    url: '/panel/networkSetting/testFavicon',
    data,
  })
}
```

- [ ] **Step 3: Add frontend types**

Create `src/typings/networkSetting.d.ts`:

```ts
declare namespace NetworkSetting {
  interface FaviconNetworkSetting {
    proxyUrl: string
    proxyFromEnv: boolean
    noProxy: string
    timeoutSeconds: number
  }

  interface FaviconNetworkTestRequest {
    url: string
    setting: FaviconNetworkSetting
  }

  interface FaviconNetworkTestResponse {
    iconUrls: string[]
    proxy: string
  }
}
```

- [ ] **Step 4: Build the NetworkSettings app**

Create `src/components/apps/NetworkSettings/index.vue`:

```vue
<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { NButton, NCard, NInput, NInputGroup, NInputNumber, NSpace, NSwitch, NTag, useMessage } from 'naive-ui'
import { getFaviconNetwork, setFaviconNetwork, testFaviconNetwork } from '@/api/panel/networkSetting'
import { t } from '@/locales'

const ms = useMessage()
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const testUrl = ref('https://github.com/lobehub/lobehub/blob/canary/README.zh-CN.md')
const testResult = ref<NetworkSetting.FaviconNetworkTestResponse | null>(null)

const form = reactive<NetworkSetting.FaviconNetworkSetting>({
  proxyUrl: '',
  proxyFromEnv: true,
  noProxy: 'localhost,127.0.0.1,::1,192.168.0.0/16,10.0.0.0/8,172.16.0.0/12',
  timeoutSeconds: 15,
})

function assignForm(setting: NetworkSetting.FaviconNetworkSetting) {
  form.proxyUrl = setting.proxyUrl || ''
  form.proxyFromEnv = setting.proxyFromEnv
  form.noProxy = setting.noProxy || ''
  form.timeoutSeconds = setting.timeoutSeconds || 15
}

async function loadSettings() {
  loading.value = true
  try {
    const res = await getFaviconNetwork<NetworkSetting.FaviconNetworkSetting>()
    if (res.code === 0)
      assignForm(res.data)
    else
      ms.error(res.msg || t('apps.networkSettings.loadFailed'))
  }
  finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    const res = await setFaviconNetwork<NetworkSetting.FaviconNetworkSetting>({ ...form })
    if (res.code === 0) {
      assignForm(res.data)
      ms.success(t('common.saveSuccess'))
    }
    else {
      ms.error(res.msg || t('common.saveFail'))
    }
  }
  finally {
    saving.value = false
  }
}

async function testSettings() {
  testing.value = true
  testResult.value = null
  try {
    const res = await testFaviconNetwork<NetworkSetting.FaviconNetworkTestResponse>({
      url: testUrl.value,
      setting: { ...form },
    })
    if (res.code === 0) {
      testResult.value = res.data
      ms.success(t('apps.networkSettings.testSuccess'))
    }
    else {
      ms.error(res.msg || t('apps.networkSettings.testFailed'))
    }
  }
  finally {
    testing.value = false
  }
}

onMounted(loadSettings)
</script>

<template>
  <div class="bg-slate-200 dark:bg-zinc-900 rounded-[10px] p-[8px] overflow-auto h-full">
    <NCard style="border-radius:10px" size="small" :loading="loading">
      <div class="text-slate-500 mb-[8px] font-bold">
        {{ $t('apps.networkSettings.faviconFetch') }}
      </div>

      <NSpace vertical size="medium">
        <div>
          <div class="mb-[5px]">
            {{ $t('apps.networkSettings.proxyUrl') }}
          </div>
          <NInput v-model:value="form.proxyUrl" clearable placeholder="http://192.168.1.2:7890" />
        </div>

        <div class="flex items-center">
          <span class="mr-[10px]">{{ $t('apps.networkSettings.proxyFromEnv') }}</span>
          <NSwitch v-model:value="form.proxyFromEnv" />
        </div>

        <div>
          <div class="mb-[5px]">
            {{ $t('apps.networkSettings.noProxy') }}
          </div>
          <NInput v-model:value="form.noProxy" type="textarea" :autosize="{ minRows: 2, maxRows: 4 }" />
        </div>

        <div>
          <div class="mb-[5px]">
            {{ $t('apps.networkSettings.timeoutSeconds') }}
          </div>
          <NInputNumber v-model:value="form.timeoutSeconds" :min="1" :max="120" />
        </div>
      </NSpace>
    </NCard>

    <NCard style="border-radius:10px" class="mt-[10px]" size="small">
      <div class="text-slate-500 mb-[8px] font-bold">
        {{ $t('apps.networkSettings.test') }}
      </div>
      <NInputGroup>
        <NInput v-model:value="testUrl" clearable placeholder="https://github.com/lobehub/lobehub/blob/canary/README.zh-CN.md" />
        <NButton :loading="testing" type="primary" @click="testSettings">
          {{ $t('apps.networkSettings.test') }}
        </NButton>
      </NInputGroup>
      <div v-if="testResult" class="mt-[10px]">
        <NTag type="success" size="small">
          {{ testResult.proxy }}
        </NTag>
        <div v-for="iconUrl in testResult.iconUrls.slice(0, 3)" :key="iconUrl" class="mt-[6px] text-xs break-all text-slate-500">
          {{ iconUrl }}
        </div>
      </div>
    </NCard>

    <NCard style="border-radius:10px" class="mt-[10px]" size="small">
      <NButton size="small" quaternary type="success" :loading="saving" @click="saveSettings">
        {{ $t('common.save') }}
      </NButton>
    </NCard>
  </div>
</template>
```

- [ ] **Step 5: Add launcher entry below Account Management**

In `src/views/home/components/AppStarter/index.vue`, update the admin-only block:

```ts
onMounted(() => {
  const adminApps: App[] = [
    {
      name: t('adminSettingUsers.appName'),
      componentName: 'Users',
      icon: 'lucide-users',
      auth: 1,
    },
    {
      name: t('apps.networkSettings.appName'),
      componentName: 'NetworkSettings',
      icon: 'lucide-network',
      auth: 1,
    },
  ]
  if (authStore.userInfo?.role === 1)
    apps.value.push(...adminApps)

  window.addEventListener('resize', handleResize)
  handleResize()
})
```

- [ ] **Step 6: Export component**

In `src/components/apps/index.ts`, add:

```ts
import NetworkSettings from './NetworkSettings/index.vue'
```

and include `NetworkSettings` in the export block.

- [ ] **Step 7: Add locale strings**

Add under `apps` in `src/locales/zh-CN.json`:

```text
"networkSettings": {
  "appName": "网络设置",
  "faviconFetch": "图标获取",
  "loadFailed": "网络设置加载失败",
  "noProxy": "不走代理",
  "proxyFromEnv": "使用环境变量代理",
  "proxyUrl": "代理地址",
  "test": "测试连接",
  "testFailed": "测试失败",
  "testSuccess": "测试成功",
  "timeoutSeconds": "超时时间（秒）"
}
```

Add under `apps` in `src/locales/en-US.json`:

```text
"networkSettings": {
  "appName": "Network Settings",
  "faviconFetch": "Favicon Fetch",
  "loadFailed": "Failed to load network settings",
  "noProxy": "No proxy",
  "proxyFromEnv": "Use environment proxy",
  "proxyUrl": "Proxy URL",
  "test": "Test",
  "testFailed": "Test failed",
  "testSuccess": "Test succeeded",
  "timeoutSeconds": "Timeout seconds"
}
```

- [ ] **Step 8: Run frontend checks**

Run:

```bash
npx pnpm@8.15.9 type-check
npx pnpm@8.15.9 build-only
```

Expected: both commands PASS.

- [ ] **Step 9: Commit frontend settings app**

Run:

```bash
git add src/api/panel/networkSetting.ts src/typings/networkSetting.d.ts src/components/apps/NetworkSettings/index.vue src/views/home/components/AppStarter/index.vue src/components/apps/index.ts src/locales/zh-CN.json src/locales/en-US.json
git commit -m "feat: add network settings app"
```

## Task 4: Final Verification and Scope Check

**Files:**
- Verify all touched files.

- [ ] **Step 1: Run GitNexus change detection**

Run:

```bash
gitnexus_detect_changes({repo: "sun-panel", scope: "all"})
```

Expected: changed symbols and affected flows match favicon networking, panel routes, and launcher UI.

- [ ] **Step 2: Run backend verification**

Run:

```bash
cd service && GOPROXY=${GOPROXY:-https://goproxy.cn,direct} go test ./...
```

Expected: PASS.

- [ ] **Step 3: Run frontend verification**

Run:

```bash
npx eslint src/components/apps/NetworkSettings/index.vue src/views/home/components/AppStarter/index.vue src/api/panel/networkSetting.ts src/typings/networkSetting.d.ts src/locales/zh-CN.json src/locales/en-US.json
npx pnpm@8.15.9 type-check
npx pnpm@8.15.9 build-only
```

Expected: targeted lint, type check, and build pass.

- [ ] **Step 4: Document known full-repo lint status**

Run:

```bash
npx pnpm@8.15.9 lint
```

Expected: FAIL with pre-existing errors in `.github/workflows/*`, `docker-compose.yml`, and `src/views/exception/test/RecursiveList.vue`. Do not fix unrelated lint errors in this feature branch.

- [ ] **Step 5: Commit any final fixes**

If verification required small fixes in the network settings feature files:

```bash
git add service/lib/siteFavicon/favico.go service/lib/siteFavicon/favico_test.go service/lib/cmn/systemSetting/systemSetting.go service/api/api_v1/panel/networkSetting.go service/api/api_v1/panel/itemIcon.go service/api/api_v1/panel/A_ENTER.go service/router/panel/networkSetting.go service/router/panel/A_ENTER.go service/initialize/config/config.go service/conf/conf.ini service/conf/conf.example.ini service/assets/conf.example.ini src/api/panel/networkSetting.ts src/typings/networkSetting.d.ts src/components/apps/NetworkSettings/index.vue src/views/home/components/AppStarter/index.vue src/components/apps/index.ts src/locales/zh-CN.json src/locales/en-US.json
git commit -m "fix: stabilize favicon network settings"
```

If no fixes are needed, do not create an empty commit.
