package siteFavicon

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	defaultTimeout = 5 * time.Second
	userAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

var fallbackIconPaths = []string{
	"/favicon.ico",
	"/favicon.png",
	"/apple-touch-icon.png",
}

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

func DefaultOptions() Options {
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
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
		},
	}
}

func IsHTTPURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "http://") ||
		strings.HasPrefix(rawURL, "https://") ||
		strings.HasPrefix(rawURL, "//")
}

func NormalizeURL(rawURL string) (*url.URL, error) {
	candidates, err := normalizeURLCandidates(rawURL)
	if err != nil {
		return nil, err
	}
	return candidates[0], nil
}

func GetOneFaviconURL(urlStr string) (string, error) {
	iconURLs, err := GetFaviconURLs(urlStr)
	if err != nil {
		return "", err
	}

	for _, v := range iconURLs {
		return v, nil
	}
	return "", fmt.Errorf("未找到图标")
}

func GetFaviconURLs(urlStr string) ([]string, error) {
	options := DefaultOptions()
	options.SkipSafetyCheck = true
	return getFaviconURL(urlStr, options)
}

func GetFaviconURLsWithOptions(urlStr string, options Options) ([]string, error) {
	return getFaviconURL(urlStr, options)
}

// 获取远程文件的大小
func GetRemoteFileSize(rawURL string) (int64, error) {
	resp, err := http.Head(rawURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// 检查HTTP响应状态
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP request failed, status code: %d", resp.StatusCode)
	}

	// 获取Content-Length字段，即文件大小
	size := resp.ContentLength
	return size, nil
}

// 下载图片
func DownloadImage(rawURL, savePath string, maxSize int64) (*os.File, error) {
	options := DefaultOptions()
	options.MaxDownloadBytes = maxSize
	options.SkipSafetyCheck = true
	return DownloadImageWithOptions(rawURL, savePath, options)
}

func DownloadImageWithOptions(rawURL, savePath string, options Options) (*os.File, error) {
	options = normalizeOptions(options)
	parsedURL, err := normalizeAndValidate(rawURL, options)
	if err != nil {
		return nil, err
	}

	client := newHTTPClient(options)
	req, err := http.NewRequest(http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	// 用受控 GET 下载，避免依赖很多家庭服务不支持的 HEAD。
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法下载图标，目标地址不可访问或超时: %w", err)
	}
	defer response.Body.Close()

	// 检查HTTP响应状态
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("图标下载失败，HTTP 状态码: %d", response.StatusCode)
	}

	if response.ContentLength > options.MaxDownloadBytes {
		return nil, fmt.Errorf("图标文件过大，最大允许 %d 字节", options.MaxDownloadBytes)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, options.MaxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取图标失败: %w", err)
	}
	if int64(len(body)) > options.MaxDownloadBytes {
		return nil, fmt.Errorf("图标文件过大，最大允许 %d 字节", options.MaxDownloadBytes)
	}

	fileExt := iconFileExt(parsedURL.Path, response.Header.Get("Content-Type"), body)
	urlFileName := path.Base(parsedURL.Path)
	if urlFileName == "." || urlFileName == "/" || urlFileName == "" {
		urlFileName = "favicon"
	}
	fileName := md5String(fmt.Sprintf("%s%s", urlFileName, time.Now().String())) + fileExt

	destination := filepath.Join(savePath, fileName)

	// 创建本地文件用于保存图片
	file, err := os.Create(destination)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// 将图片数据写入本地文件
	if _, err = file.Write(body); err != nil {
		_ = os.Remove(destination)
		return nil, err
	}
	return file, nil
}

func GetOneFaviconURLAndUpload(urlStr string) (string, bool) {
	//www.iqiyipic.com/pcwimg/128-128-logo.png
	iconURLs, err := GetFaviconURLs(urlStr)
	if err != nil {
		return "", false
	}

	for _, v := range iconURLs {
		return v, true
	}
	return "", false
}

func getFaviconURL(rawURL string, options Options) ([]string, error) {
	options = normalizeOptions(options)
	candidates, err := normalizeURLCandidates(rawURL)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, candidate := range candidates {
		if !options.SkipSafetyCheck {
			if err := validateParsedURLSafety(candidate, options); err != nil {
				lastErr = err
				continue
			}
		}
		icons, err := discoverFaviconURLs(candidate, options)
		if err == nil {
			return icons, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("未找到图标")
}

func preserveProxyRootRequestURI(req *http.Request, options Options) {
	if req == nil || req.URL == nil || req.URL.Path != "" || req.URL.RawQuery != "" {
		return
	}
	if strings.TrimSpace(options.ProxyURL) == "" || shouldBypassProxy(req.URL, options.NoProxy) {
		return
	}
	req.URL.Opaque = "//" + req.URL.Host
}

func discoverFaviconURLs(pageURL *url.URL, options Options) ([]string, error) {
	candidates := make([]iconCandidate, 0)
	manifestURLs := make([]*url.URL, 0)
	seen := make(map[string]bool)
	client := newHTTPClient(options)
	req, err := http.NewRequest(http.MethodGet, pageURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	preserveProxyRootRequestURI(req, options)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("目标页面不可访问或超时: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("目标页面访问失败，HTTP 状态码: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	addIcon := func(baseURL *url.URL, rawHref, rel, iconType, sizes, source, purpose string) {
		rawHref = strings.TrimSpace(rawHref)
		if rawHref == "" {
			return
		}
		ref, err := url.Parse(rawHref)
		if err != nil {
			return
		}
		resolved := baseURL.ResolveReference(ref)
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			return
		}
		resolved.Fragment = ""
		value := resolved.String()
		if seen[value] {
			return
		}
		seen[value] = true
		candidates = append(candidates, iconCandidate{
			URL:     value,
			Rel:     rel,
			Type:    iconType,
			Sizes:   sizes,
			Source:  source,
			Purpose: purpose,
			Order:   len(candidates),
		})
	}

	addManifest := func(rawHref string) {
		rawHref = strings.TrimSpace(rawHref)
		if rawHref == "" {
			return
		}
		ref, err := url.Parse(rawHref)
		if err != nil {
			return
		}
		resolved := pageURL.ResolveReference(ref)
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			return
		}
		manifestURLs = append(manifestURLs, resolved)
	}

	// 查找所有link标签，筛选包含rel属性为"icon"的标签。
	doc.Find("link").Each(func(i int, s *goquery.Selection) {
		rel, _ := s.Attr("rel")
		href, _ := s.Attr("href")
		iconType, _ := s.Attr("type")
		sizes, _ := s.Attr("sizes")
		relLower := strings.ToLower(rel)

		if strings.Contains(relLower, "manifest") {
			addManifest(href)
		}
		if strings.Contains(relLower, "icon") && href != "" {
			addIcon(pageURL, href, rel, iconType, sizes, "html", "")
		}
	})

	for _, manifestURL := range manifestURLs {
		for _, manifestIcon := range fetchManifestIconCandidates(manifestURL, client, options) {
			addIcon(manifestURL, manifestIcon.Src, "manifest", manifestIcon.Type, manifestIcon.Sizes, "manifest", manifestIcon.Purpose)
		}
	}

	for _, fallbackPath := range fallbackIconPaths {
		fallbackURL := &url.URL{
			Scheme: pageURL.Scheme,
			Host:   pageURL.Host,
			Path:   fallbackPath,
		}
		addIcon(pageURL, fallbackURL.String(), "icon", inferIconTypeFromURL(fallbackURL.Path), "", "fallback", "")
	}

	if len(candidates) == 0 {
		return nil, errors.New("未找到图标")
	}

	sortIconCandidates(candidates)
	icons := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		icons = append(icons, candidate.URL)
	}
	return icons, nil
}

type iconCandidate struct {
	URL     string
	Rel     string
	Type    string
	Sizes   string
	Source  string
	Purpose string
	Order   int
}

type webManifest struct {
	Icons []manifestIcon `json:"icons"`
}

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

func fetchManifestIconCandidates(manifestURL *url.URL, client *http.Client, options Options) []manifestIcon {
	if !options.SkipSafetyCheck {
		if err := validateParsedURLSafety(manifestURL, options); err != nil {
			return nil
		}
	}

	req, err := http.NewRequest(http.MethodGet, manifestURL.String(), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil
	}

	manifest := webManifest{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256*1024)).Decode(&manifest); err != nil {
		return nil
	}
	return manifest.Icons
}

func sortIconCandidates(candidates []iconCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		leftScore := iconCandidateScore(candidates[i])
		rightScore := iconCandidateScore(candidates[j])
		if leftScore == rightScore {
			return candidates[i].Order < candidates[j].Order
		}
		return leftScore > rightScore
	})
}

func iconCandidateScore(candidate iconCandidate) int {
	if candidate.Source == "fallback" {
		return 1000 - candidate.Order
	}

	score := 0
	iconType := strings.ToLower(strings.TrimSpace(candidate.Type))
	if iconType == "" {
		iconType = inferIconTypeFromURL(candidate.URL)
	}

	switch iconType {
	case "image/svg+xml":
		score += 10000
	case "image/png", "image/webp":
		score += 8000
	case "image/x-icon", "image/vnd.microsoft.icon":
		score += 3000
	case "image/jpeg", "image/jpg":
		score += 2500
	default:
		score += 1000
	}

	rel := strings.ToLower(candidate.Rel)
	if strings.Contains(rel, "apple-touch-icon") {
		score += 1200
	}
	switch candidate.Source {
	case "manifest":
		score += 800
	case "html":
		score += 500
	case "fallback":
		score -= 500
	}

	if size := parseIconSize(candidate.Sizes); size > 0 {
		if size >= 64 && size <= 192 {
			score += 1000
		}
		score += 512 - abs(size-128)
	} else if iconType == "image/svg+xml" {
		score += 900
	}

	purpose := strings.ToLower(candidate.Purpose)
	if strings.Contains(purpose, "maskable") && !strings.Contains(purpose, "any") {
		score -= 1500
	}

	if strings.HasSuffix(strings.ToLower(candidate.URL), "/favicon.ico") {
		score -= 300
	}
	return score
}

func parseIconSize(sizes string) int {
	best := 0
	for _, size := range strings.Fields(strings.ToLower(sizes)) {
		if size == "any" {
			continue
		}
		parts := strings.Split(size, "x")
		if len(parts) != 2 {
			continue
		}
		width, errW := strconv.Atoi(parts[0])
		height, errH := strconv.Atoi(parts[1])
		if errW != nil || errH != nil {
			continue
		}
		dimension := width
		if height > dimension {
			dimension = height
		}
		if dimension > best {
			best = dimension
		}
	}
	return best
}

func inferIconTypeFromURL(rawURL string) string {
	switch strings.ToLower(path.Ext(rawURL)) {
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".ico":
		return "image/x-icon"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func normalizeURLCandidates(rawURL string) ([]*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, errors.New("URL 不能为空")
	}

	if strings.HasPrefix(trimmed, "//") {
		return parseHTTPURLCandidates([]string{"http:" + trimmed})
	}

	if strings.Contains(trimmed, "://") {
		return parseHTTPURLCandidates([]string{trimmed})
	}

	if looksLikeUnsupportedScheme(trimmed) {
		return nil, errors.New("仅支持 HTTP/HTTPS URL")
	}

	if isLikelyHTTPFirst(trimmed) {
		return parseHTTPURLCandidates([]string{"http://" + trimmed, "https://" + trimmed})
	}
	return parseHTTPURLCandidates([]string{"https://" + trimmed, "http://" + trimmed})
}

func ValidateURLSafety(rawURL string, options Options) (*url.URL, error) {
	options = normalizeOptions(options)
	return normalizeAndValidate(rawURL, options)
}

func normalizeAndValidate(rawURL string, options Options) (*url.URL, error) {
	candidates, err := normalizeURLCandidates(rawURL)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, candidate := range candidates {
		if options.SkipSafetyCheck {
			return candidate, nil
		}
		if err := validateParsedURLSafety(candidate, options); err != nil {
			lastErr = err
			continue
		}
		return candidate, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("URL 安全校验失败")
}

func validateParsedURLSafety(parsedURL *url.URL, options Options) error {
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return errors.New("仅支持 HTTP/HTTPS URL")
	}
	if parsedURL.Host == "" {
		return errors.New("URL 缺少主机名")
	}

	host := parsedURL.Hostname()
	lowerHost := strings.ToLower(strings.Trim(host, "[]"))
	for _, denied := range options.DenyHosts {
		if lowerHost == strings.ToLower(strings.TrimSpace(denied)) {
			if lowerHost == "localhost" || lowerHost == "127.0.0.1" || lowerHost == "::1" {
				return errors.New("Docker 容器中的 localhost 不是你的电脑或 NAS 宿主机，请使用 NAS 在局域网中的真实 IP 或 Docker 网络可访问地址")
			}
			return fmt.Errorf("目标主机被安全策略拦截: %s", host)
		}
	}

	ips := make([]net.IP, 0)
	if ip := net.ParseIP(lowerHost); ip != nil {
		ips = append(ips, ip)
	} else {
		resolved, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("无法解析目标主机: %w", err)
		}
		ips = append(ips, resolved...)
	}
	if len(ips) == 0 {
		return errors.New("无法解析目标主机")
	}

	for _, ip := range ips {
		if err := validateIPSafety(ip, options); err != nil {
			return err
		}
	}
	return nil
}

func validateIPSafety(ip net.IP, options Options) error {
	if ip == nil {
		return errors.New("目标 IP 无效")
	}
	if ip.Equal(net.IPv4(169, 254, 169, 254)) {
		return errors.New("目标地址 169.254.169.254 被安全策略拦截")
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("目标地址 %s 被安全策略拦截", ip.String())
	}
	if ip.Equal(net.IPv4(255, 255, 255, 255)) {
		return fmt.Errorf("目标地址 %s 被安全策略拦截", ip.String())
	}
	if ip.IsLoopback() {
		if cidrContainsIP(options.AllowCIDRs, ip) {
			return nil
		}
		return errors.New("Docker 容器中的 localhost 不是你的电脑或 NAS 宿主机，请使用 NAS 在局域网中的真实 IP 或 Docker 网络可访问地址")
	}
	if ip.IsPrivate() {
		if !options.AllowPrivateNetwork {
			return fmt.Errorf("内网地址 %s 被安全策略拦截", ip.String())
		}
		if !cidrContainsIP(options.AllowCIDRs, ip) {
			return fmt.Errorf("内网地址 %s 不在允许的 CIDR 范围内", ip.String())
		}
	}
	return nil
}

func cidrContainsIP(cidrs []string, ip net.IP) bool {
	for _, rawCIDR := range cidrs {
		rawCIDR = strings.TrimSpace(rawCIDR)
		if rawCIDR == "" {
			continue
		}
		_, network, err := net.ParseCIDR(rawCIDR)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

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

func parseProxyURL(rawProxyURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawProxyURL)
	if trimmed == "" {
		return nil, nil
	}
	proxyURL, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("代理 URL 格式无效: %w", err)
	}
	if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
		return nil, errors.New("仅支持 HTTP/HTTPS 代理")
	}
	if proxyURL.Host == "" {
		return nil, errors.New("代理 URL 缺少主机名")
	}
	return proxyURL, nil
}

func proxyFunc(options Options) func(*http.Request) (*url.URL, error) {
	envProxy := http.ProxyFromEnvironment
	return func(req *http.Request) (*url.URL, error) {
		if shouldBypassProxy(req.URL, options.NoProxy) {
			return nil, nil
		}

		if strings.TrimSpace(options.ProxyURL) != "" {
			return parseProxyURL(options.ProxyURL)
		}
		if options.ProxyFromEnv {
			return envProxy(req)
		}
		return nil, nil
	}
}

func shouldBypassProxy(targetURL *url.URL, noProxy []string) bool {
	if targetURL == nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(targetURL.Hostname()))
	if host == "" {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))

	for _, rule := range noProxy {
		rule = strings.ToLower(strings.TrimSpace(rule))
		if rule == "" {
			continue
		}
		if rule == "*" {
			return true
		}
		if _, network, err := net.ParseCIDR(rule); err == nil {
			if ip != nil && network.Contains(ip) {
				return true
			}
			continue
		}
		if ruleIP := net.ParseIP(strings.Trim(rule, "[]")); ruleIP != nil {
			if ip != nil && ip.Equal(ruleIP) {
				return true
			}
			continue
		}

		domainRule := strings.TrimPrefix(rule, ".")
		if host == domainRule || strings.HasSuffix(host, "."+domainRule) {
			return true
		}
	}
	return false
}

func normalizeOptions(options Options) Options {
	defaults := DefaultOptions()
	if options.Timeout == 0 {
		options.Timeout = defaults.Timeout
	}
	if options.MaxDownloadBytes == 0 {
		options.MaxDownloadBytes = defaults.MaxDownloadBytes
	}
	if options.MaxRedirects == 0 {
		options.MaxRedirects = defaults.MaxRedirects
	}
	if options.AllowCIDRs == nil {
		options.AllowCIDRs = defaults.AllowCIDRs
	}
	if options.DenyHosts == nil {
		options.DenyHosts = defaults.DenyHosts
	}
	if options.NoProxy == nil {
		options.NoProxy = defaults.NoProxy
	}
	return options
}

func parseHTTPURLCandidates(values []string) ([]*url.URL, error) {
	result := make([]*url.URL, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		parsed, err := url.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("URL 格式无效: %w", err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, errors.New("仅支持 HTTP/HTTPS URL")
		}
		if parsed.Host == "" {
			return nil, errors.New("URL 缺少主机名")
		}
		parsed.Fragment = ""
		key := parsed.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, parsed)
	}
	if len(result) == 0 {
		return nil, errors.New("URL 格式无效")
	}
	return result, nil
}

func looksLikeUnsupportedScheme(rawURL string) bool {
	schemeEnd := strings.Index(rawURL, ":")
	if schemeEnd <= 0 {
		return false
	}
	pathStart := strings.IndexAny(rawURL, "/?#")
	if pathStart >= 0 && pathStart < schemeEnd {
		return false
	}
	if isHostPort(rawURL[:schemeEnd], rawURL[schemeEnd+1:]) {
		return false
	}
	return true
}

func isHostPort(host, rest string) bool {
	if host == "" || rest == "" {
		return false
	}
	port := rest
	if idx := strings.IndexAny(port, "/?#"); idx >= 0 {
		port = port[:idx]
	}
	if port == "" {
		return false
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isLikelyHTTPFirst(rawURL string) bool {
	host := rawURL
	if idx := strings.IndexAny(host, "/?#"); idx >= 0 {
		host = host[:idx]
	}
	if strings.Contains(host, ":") {
		return true
	}
	host = strings.Trim(host, "[]")
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" ||
		strings.HasSuffix(lowerHost, ".local") ||
		strings.HasSuffix(lowerHost, ".lan") {
		return true
	}
	if net.ParseIP(host) != nil {
		return true
	}
	return !strings.Contains(host, ".")
}

func iconFileExt(urlPath, contentType string, body []byte) string {
	if ext := strings.ToLower(path.Ext(urlPath)); isSupportedImageExt(ext) {
		return ext
	}

	if contentType != "" {
		if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
			switch strings.ToLower(mediaType) {
			case "image/png":
				return ".png"
			case "image/jpeg":
				return ".jpg"
			case "image/gif":
				return ".gif"
			case "image/webp":
				return ".webp"
			case "image/svg+xml":
				return ".svg"
			case "image/x-icon", "image/vnd.microsoft.icon":
				return ".ico"
			}
		}
	}

	if len(body) >= 8 && bytesHasPrefix(body, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return ".png"
	}
	if len(body) >= 3 && bytesHasPrefix(body, []byte{0xff, 0xd8, 0xff}) {
		return ".jpg"
	}
	if len(body) >= 6 && (bytesHasPrefix(body, []byte("GIF87a")) || bytesHasPrefix(body, []byte("GIF89a"))) {
		return ".gif"
	}
	if len(body) >= 12 && bytesHasPrefix(body[:4], []byte("RIFF")) && bytesHasPrefix(body[8:12], []byte("WEBP")) {
		return ".webp"
	}
	snippet := strings.ToLower(string(body[:min(len(body), 256)]))
	if strings.Contains(snippet, "<svg") {
		return ".svg"
	}
	return ".ico"
}

func isSupportedImageExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".svg":
		return true
	default:
		return false
	}
}

func bytesHasPrefix(value, prefix []byte) bool {
	if len(value) < len(prefix) {
		return false
	}
	for i := range prefix {
		if value[i] != prefix[i] {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func md5String(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func HashFile(filePath string) (string, int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}

	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}
