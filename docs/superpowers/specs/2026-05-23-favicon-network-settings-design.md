# Favicon Network Settings Design

## Goal

Add an administrator-only network settings panel for favicon fetching. The setting should help users in regions with unstable access to GitHub and other foreign sites configure an HTTP/HTTPS proxy from the web UI, while preserving the current LAN/NAS favicon behavior.

## Placement

The panel appears in the system app launcher below the existing account management entry. It is visible only to administrators, matching the current account management visibility rule.

Label:

- zh-CN: `网络设置`
- en-US: `Network Settings`

## Settings

The first version manages only favicon-fetch networking:

- `proxyUrl`: optional HTTP/HTTPS proxy URL, such as `http://192.168.1.2:7890`.
- `proxyFromEnv`: when enabled and `proxyUrl` is empty, use `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` from the process environment.
- `noProxy`: comma-separated hosts, IPs, and CIDR ranges that should bypass the proxy. Defaults include localhost and private LAN ranges.
- `timeoutSeconds`: request timeout for target page and icon download requests. Default is 15 seconds.

SOCKS5 is intentionally out of scope for the first version because most Clash/OpenClash/mihomo deployments expose an HTTP proxy port, and HTTP proxy support keeps the implementation smaller and easier to test.

## Backend Storage

Persist these values in the existing `system_settings` table under a new config key:

`favicon_network`

The backend keeps `conf.ini` values as deployment defaults. Runtime resolution order:

1. `favicon_network` from `system_settings`
2. `[favicon]` values from `conf.ini`
3. code defaults

Proxy selection order:

1. If `proxyUrl` is non-empty, use that proxy.
2. If `proxyUrl` is empty and `proxyFromEnv` is true, use environment proxy variables.
3. Otherwise use direct connections.

`noProxy` applies to both configured proxy and environment proxy paths so LAN/NAS addresses do not get routed through a foreign proxy.

## API

Add administrator-only endpoints under the existing authenticated panel API group:

- `POST /panel/networkSetting/getFavicon`
- `POST /panel/networkSetting/setFavicon`
- `POST /panel/networkSetting/testFavicon`

Each handler checks that the current user is an administrator before reading or changing the global setting.

The test endpoint should return whether the backend can reach and discover favicon candidates for the supplied URL using the effective settings. It should not persist anything unless the user clicks save.

## Frontend

Create a `NetworkSettings` app component using existing Naive UI patterns:

- input for proxy URL
- switch for environment proxy fallback
- input or textarea for no-proxy list
- numeric input for timeout seconds
- test URL input and test button
- save button

The form should avoid explanatory wall text. Use placeholders and compact labels.

## Error Handling

Invalid proxy URLs are rejected with a clear validation error.

Timeouts and unreachable target pages should include the target host and whether a proxy was configured, without leaking proxy credentials.

If the system setting row is missing, the get endpoint returns effective defaults.

## Testing

Backend tests cover:

- config resolution order
- proxy URL validation
- no-proxy matching for localhost and private CIDR ranges
- HTTP client using a configured proxy
- test endpoint behavior with a local HTTP server

Frontend verification covers:

- administrator sees the Network Settings app under account management
- non-admin users do not see it
- save and test states render correctly
