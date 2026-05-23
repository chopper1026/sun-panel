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
