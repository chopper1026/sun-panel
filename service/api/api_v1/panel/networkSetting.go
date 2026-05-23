package panel

import (
	"errors"
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

const (
	defaultFaviconNoProxy        = "localhost,127.0.0.1,::1,192.168.0.0/16,10.0.0.0/8,172.16.0.0/12"
	defaultFaviconTimeoutSeconds = 15
	maxFaviconTimeoutSeconds     = 120
)

type NetworkSetting struct{}

type faviconNetworkTestReq struct {
	URL     string                              `json:"url" binding:"required"`
	Setting systemSetting.FaviconNetworkSetting `json:"setting"`
}

type faviconNetworkTestResp struct {
	IconURLs []string `json:"iconUrls"`
	Proxy    string   `json:"proxy"`
}

func (a *NetworkSetting) GetFavicon(c *gin.Context) {
	setting, err := getEffectiveFaviconNetworkSetting()
	if err != nil {
		apiReturn.ErrorDatabase(c, err.Error())
		return
	}
	normalized, err := normalizeFaviconNetworkSetting(setting)
	if err != nil {
		apiReturn.Error(c, err.Error())
		return
	}
	apiReturn.SuccessData(c, normalized)
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
	if global.SystemSetting == nil {
		apiReturn.ErrorDatabase(c, "system setting cache is not initialized")
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

	normalized, err := normalizeFaviconNetworkSetting(req.Setting)
	if err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}
	iconURLs, err := siteFavicon.GetFaviconURLsWithOptions(req.URL, getFaviconOptionsFromSetting(normalized))
	if err != nil {
		apiReturn.Error(c, "图标获取失败："+err.Error())
		return
	}

	apiReturn.SuccessData(c, faviconNetworkTestResp{
		IconURLs: iconURLs,
		Proxy:    safeProxyLabel(normalized),
	})
}

func getFaviconNetworkSettingFromConfig() systemSetting.FaviconNetworkSetting {
	setting := systemSetting.FaviconNetworkSetting{
		ProxyFromEnv:   true,
		NoProxy:        defaultFaviconNoProxy,
		TimeoutSeconds: defaultFaviconTimeoutSeconds,
	}
	if global.Config == nil {
		return setting
	}

	setting.ProxyURL = strings.TrimSpace(global.Config.GetValueStringOrDefault("favicon", "proxy_url"))
	if proxyFromEnv := strings.TrimSpace(global.Config.GetValueStringOrDefault("favicon", "proxy_from_env")); proxyFromEnv != "" {
		if parsed, err := strconv.ParseBool(proxyFromEnv); err == nil {
			setting.ProxyFromEnv = parsed
		}
	}
	if noProxy := strings.TrimSpace(global.Config.GetValueStringOrDefault("favicon", "no_proxy")); noProxy != "" {
		setting.NoProxy = noProxy
	}
	if timeoutSeconds, err := strconv.Atoi(strings.TrimSpace(global.Config.GetValueStringOrDefault("favicon", "timeout_seconds"))); err == nil && timeoutSeconds > 0 {
		setting.TimeoutSeconds = timeoutSeconds
	}
	return setting
}

func getEffectiveFaviconNetworkSetting() (systemSetting.FaviconNetworkSetting, error) {
	setting := getFaviconNetworkSettingFromConfig()
	if global.SystemSetting == nil {
		return setting, nil
	}

	storedSetting := systemSetting.FaviconNetworkSetting{}
	err := global.SystemSetting.GetValueByInterface(systemSetting.FAVICON_NETWORK, &storedSetting)
	if err == nil {
		return mergeFaviconNetworkSetting(setting, storedSetting), nil
	}
	if errors.Is(err, systemSetting.ErrorNoExists) {
		return setting, nil
	}
	return setting, err
}

func mergeFaviconNetworkSetting(base, override systemSetting.FaviconNetworkSetting) systemSetting.FaviconNetworkSetting {
	result := base
	result.ProxyURL = strings.TrimSpace(override.ProxyURL)
	result.ProxyFromEnv = override.ProxyFromEnv
	if strings.TrimSpace(override.NoProxy) != "" {
		result.NoProxy = override.NoProxy
	}
	if override.TimeoutSeconds > 0 {
		result.TimeoutSeconds = override.TimeoutSeconds
	}
	return result
}

func normalizeFaviconNetworkSetting(setting systemSetting.FaviconNetworkSetting) (systemSetting.FaviconNetworkSetting, error) {
	setting.ProxyURL = strings.TrimSpace(setting.ProxyURL)
	setting.NoProxy = strings.TrimSpace(setting.NoProxy)
	if setting.NoProxy == "" {
		setting.NoProxy = defaultFaviconNoProxy
	}
	if setting.TimeoutSeconds <= 0 {
		setting.TimeoutSeconds = defaultFaviconTimeoutSeconds
	}
	if setting.TimeoutSeconds > maxFaviconTimeoutSeconds {
		setting.TimeoutSeconds = maxFaviconTimeoutSeconds
	}

	if setting.ProxyURL != "" {
		proxyURL, err := url.Parse(setting.ProxyURL)
		if err != nil {
			return setting, fmt.Errorf("代理 URL 格式无效: %w", err)
		}
		if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
			return setting, errors.New("仅支持 HTTP/HTTPS 代理")
		}
		if proxyURL.Host == "" {
			return setting, errors.New("代理 URL 缺少主机名")
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

	if global.Config == nil {
		return options
	}
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
		return "none"
	}

	proxyURL, err := url.Parse(setting.ProxyURL)
	if err != nil {
		return setting.ProxyURL
	}
	if proxyURL.User != nil {
		if _, hasPassword := proxyURL.User.Password(); hasPassword {
			proxyURL.User = url.UserPassword("***", "***")
		} else {
			proxyURL.User = url.User("***")
		}
	}
	return proxyURL.String()
}
