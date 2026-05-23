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
