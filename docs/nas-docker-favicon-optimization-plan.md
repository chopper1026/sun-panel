# NAS + Docker 场景下的书签图标获取优化方案

## 背景

当前项目添加/编辑书签时，点击“获取图标”会由前端调用后端接口 `/panel/itemIcon/getSiteFavicon`，后端访问目标网站，解析页面中的 favicon 链接，下载图标到本地 `source_path` 目录，并把文件记录写入数据库。

核心链路：

- 前端入口：`src/views/home/components/EditItem/index.vue` 的 `getIconByUrl`
- API 封装：`src/api/panel/itemIcon.ts` 的 `getSiteFavicon`
- 后端路由：`service/router/panel/itemIcon.go` 的 `/panel/itemIcon/getSiteFavicon`
- 后端处理：`service/api/api_v1/panel/itemIcon.go` 的 `GetSiteFavicon`
- favicon 解析与下载：`service/lib/siteFavicon/favico.go`

你的实际部署方式是：项目运行在家庭 NAS 的 Docker 容器中，你在局域网电脑上通过 `NAS_IP:项目端口` 访问面板。因此，图标获取请求实际从 Docker 容器发出，而不是从浏览器发出。这会影响局域网地址、反向代理、容器网络和安全策略的设计。

## 当前实现的问题

### 1. 成功率不足

当前后端只解析目标页面中的 `<link rel="...icon...">`，并取第一个匹配项。很多服务没有显式声明 favicon，或者只提供 `/favicon.ico`、`/favicon.png`、`manifest.webmanifest`、`apple-touch-icon`，当前逻辑会直接失败。

### 2. 对家庭局域网地址不友好

很多 NAS 服务地址是：

- `192.168.1.20:5000`
- `10.0.0.5:8080`
- `nas.local:3000`
- `http://openwrt.lan`

当前要求 URL 能被 Go 的 `http.NewRequest` 直接接受，未补协议的地址会失败。并且 `localhost` 在 Docker 容器里代表容器自身，不代表你的电脑或 NAS 宿主机，容易产生误解。

### 3. 下载流程过度依赖 HEAD

当前 `DownloadImage` 会先发 `HEAD` 请求读取 `Content-Length`，再发 `GET` 下载。很多家庭服务、Docker Web UI、反向代理、低配应用不支持 HEAD，导致实际 GET 可以下载但当前逻辑失败。

### 4. 缺少网络超时和下载硬限制

当前 HTTP 请求没有明确超时。若目标服务不响应，接口可能长时间阻塞。当前大小限制也依赖 `Content-Length`，如果服务端不返回或返回不准，实际下载阶段没有硬限制。

### 5. SSRF 风险需要按家庭部署场景重新设计

普通公网应用通常禁止访问私有 IP，防止 SSRF。但你的核心使用场景就是访问局域网服务，所以不能简单禁止 `192.168.0.0/16`、`10.0.0.0/8` 等地址。

更合理的策略是：允许家庭局域网地址，但禁止明显危险或无意义的地址，例如 `127.0.0.1`、`::1`、`169.254.169.254`、multicast、unspecified 地址，以及 Docker 内部敏感地址。

### 6. 重复文件和孤儿文件会增长

每次点击“获取图标”都会保存新文件并写入 `files` 表。即使用户最后没有保存书签，也会留下文件记录。重复点击同一个站点也会产生重复图标。

### 7. 错误提示不可操作

前端统一显示“图标获取失败”，无法区分 URL 无效、NAS 容器无法访问目标地址、目标无 favicon、下载超时、文件过大、类型不支持等情况。

## 优化目标

1. 在 NAS + Docker + 局域网部署中，提高常见内网服务图标获取成功率。
2. 保持“下载到本地”的行为，避免面板运行时依赖远程 favicon。
3. 支持无协议局域网地址自动补全。
4. 对 SSRF 做家庭部署友好的防护，而不是简单禁用内网。
5. 减少重复下载和孤儿文件。
6. 给用户明确、可操作的失败原因。

## 推荐架构

保留当前“服务端抓取并落盘”的总体架构，但将 favicon 获取流程拆成几个明确阶段：

1. URL 规范化
2. 地址安全校验
3. 候选图标发现
4. 候选排序与选择
5. 受控下载
6. 本地缓存与去重
7. 返回本地可访问路径

建议新增或重构为独立服务，例如：

- `service/lib/siteFavicon/normalize.go`：URL 规范化
- `service/lib/siteFavicon/security.go`：地址安全校验
- `service/lib/siteFavicon/discover.go`：候选图标发现
- `service/lib/siteFavicon/download.go`：受控下载
- `service/lib/siteFavicon/cache.go`：缓存与去重逻辑

如果不想拆太多文件，也至少应把 `favico.go` 中的解析、下载、安全校验分成独立函数，便于测试。

## 详细方案

### 1. URL 规范化

输入 URL 先进行规范化，降低用户填写成本。

规则：

- `example.com` 转为 `https://example.com`，失败时再尝试 `http://example.com`。
- `192.168.1.20:5000` 默认转为 `http://192.168.1.20:5000`，因为家庭内网服务更常见 HTTP。
- 已有 `http://` 或 `https://` 的地址保持不变。
- 去除首尾空格。
- 拒绝非 HTTP 协议，例如 `file://`、`ftp://`、`gopher://`、`data:`。

建议策略：

- 对公网域名优先尝试 HTTPS。
- 对 IP 地址、`.local`、`.lan`、无证书内网域名优先尝试 HTTP。
- 如果用户输入 `localhost` 或 `127.0.0.1`，返回明确提示：Docker 容器中的 localhost 不是你的电脑或 NAS 宿主机，请使用 NAS 在局域网中的真实 IP 或 Docker 网络可访问地址。

### 2. 家庭部署友好的安全校验

不建议完全禁止私网地址，因为这会破坏你的 NAS 使用场景。

默认允许：

- `192.168.0.0/16`
- `10.0.0.0/8`
- `172.16.0.0/12`
- 合法公网地址
- 可解析到上述地址的家庭内网域名，例如 `nas.local`、`openwrt.lan`

默认禁止：

- `127.0.0.0/8`
- `::1`
- `0.0.0.0/8`
- link-local：`169.254.0.0/16`
- 云 metadata：`169.254.169.254`
- multicast、broadcast、unspecified 地址
- 非 HTTP/HTTPS 协议

可选配置：

在配置文件中增加白名单/黑名单，便于高级用户调整：

```ini
[favicon]
timeout_seconds=5
max_download_bytes=1048576
allow_private_network=true
allow_cidrs=192.168.0.0/16,10.0.0.0/8,172.16.0.0/12
deny_hosts=localhost,127.0.0.1,::1,169.254.169.254
```

安全重点：

- 域名解析后要检查最终 IP。
- HTTP 重定向后的目标地址也要再次检查。
- 不允许从公网 URL 重定向到禁止地址。

### 3. 候选图标发现

当前只解析 `<link rel="...icon...">`，需要扩展为多级候选。

建议顺序：

1. 请求目标页面 HTML。
2. 解析 `<link>` 标签：
   - `rel="icon"`
   - `rel="shortcut icon"`
   - `rel="apple-touch-icon"`
   - `rel="apple-touch-icon-precomposed"`
   - `rel="mask-icon"`
3. 解析 `sizes`、`type`、`href`。
4. 如果页面声明了 `manifest.webmanifest` 或 `manifest.json`，解析其中的 `icons`。
5. 如果页面没有候选，尝试 fallback：
   - `/favicon.ico`
   - `/favicon.png`
   - `/apple-touch-icon.png`

相对路径解析必须使用 `url.ResolveReference`，不能手动拼字符串。这样才能正确处理：

- `/favicon.ico`
- `favicon.ico`
- `../favicon.png`
- `//cdn.example.com/favicon.ico`
- 带路径的页面 URL，例如 `http://host/app/login`

### 4. 候选排序

不要简单取第一个。建议按可显示质量排序：

优先级：

1. SVG，适合缩放，但要注意安全渲染和 MIME 校验。
2. PNG/WebP，优先 64x64、128x128、180x180、192x192。
3. ICO，作为兼容 fallback。
4. JPG/JPEG，低优先级。

排序参考：

- 明确 `sizes` 且尺寸接近 64-192 的优先。
- `type=image/svg+xml` 优先，但要限制文件大小。
- `apple-touch-icon` 通常质量较高，可高于默认 `favicon.ico`。
- 如果候选来自 `manifest`，按 `purpose` 和尺寸选择普通图标，避免只选 maskable 导致显示异常。

### 5. 受控下载

用 GET 替代强依赖 HEAD。

下载要求：

- HTTP client 设置超时，例如 5 秒。
- 设置 User-Agent，模拟普通浏览器。
- 限制最大重定向次数，例如 3 次。
- 每次重定向后重新做安全校验。
- 用 `io.LimitedReader` 或等价方式限制真实读取字节数。
- 默认最大下载 1MB，favicon 场景足够。
- 校验响应状态码必须是 2xx。
- 校验 `Content-Type` 或文件头，允许：
  - `image/png`
  - `image/jpeg`
  - `image/gif`
  - `image/webp`
  - `image/x-icon`
  - `image/vnd.microsoft.icon`
  - `image/svg+xml`

如果 `Content-Type` 缺失，可以根据文件头和扩展名做兜底，但不能完全相信扩展名。

### 6. 缓存与去重

建议增加 favicon 缓存，避免重复下载。

缓存 key：

- 首选：最终图标 URL 规范化后的字符串。
- 备选：`scheme + host + icon path`。

缓存字段建议：

- `source_url`
- `final_icon_url`
- `local_src`
- `content_hash`
- `content_type`
- `size`
- `created_at`
- `updated_at`
- `last_used_at`

如果不想新增表，可以先在现有 `files` 表中复用 `FileName` 或新增轻量字段。但更清晰的方案是新增 favicon cache 表。

去重策略：

- 下载前先查相同 `final_icon_url` 是否已有可用本地文件。
- 下载后计算内容 hash，若文件内容已存在，删除新文件并复用旧记录。
- 每次复用时更新 `last_used_at`。

### 7. 避免孤儿文件

当前点击“获取图标”就会落盘，用户不保存书签也会留下文件。

有两个可选方案：

方案 A：临时文件机制

- 获取图标时先保存到临时目录或标记为临时文件。
- 用户保存书签后转为正式文件。
- 定时清理超过 24 小时未绑定书签的临时文件。

方案 B：缓存文件机制

- 获取图标即进入 favicon cache。
- 不要求一定绑定书签。
- 定期清理长时间未使用且无书签引用的缓存。

推荐方案 B，因为 favicon 天然适合缓存，重复使用价值高。

### 8. 前端交互优化

当前只有一个“获取图标”按钮。建议保持简单，但提高反馈质量。

最小改动：

- URL 未填协议时，前端不阻止，让后端规范化。
- 后端返回具体错误消息，前端展示：
  - URL 格式无效
  - Docker 容器无法访问该地址
  - 未找到图标
  - 图标文件过大
  - 图标格式不支持
  - 访问被安全策略拦截

可选增强：

- 返回多个候选图标，前端让用户选择。
- 默认只展示最佳候选，提供“更多图标”展开。
- 对 `localhost` 给专门提示。

考虑到项目当前 UI 比较轻量，第一阶段不建议做候选选择界面，先后端自动选最佳图标。

### 9. Docker 部署建议

必须保证上传目录持久化。

示例：

```yaml
services:
  sun-panel:
    image: sun-panel
    ports:
      - '3002:3002'
    volumes:
      - ./data/uploads:/app/uploads
      - ./data/conf:/app/conf
```

注意：

- `source_path` 对应目录必须挂载 volume。
- 容器需要能访问家庭局域网目标服务。
- 如果 Docker 使用 bridge 网络，访问 NAS 宿主机服务可能需要使用 NAS 的局域网 IP，而不是 `localhost`。
- 如果目标服务只监听 `127.0.0.1`，容器访问不到，需要改为监听 NAS 局域网 IP 或使用合适的 Docker 网络模式。

## 分阶段实施计划

### 第一阶段：提高成功率和稳定性

目标：不大改数据结构，优先提升局域网服务图标获取成功率。

状态：已完成（2026-05-23）。

完成记录：

- 后端新增 URL 规范化逻辑，无协议 `host:port` 默认优先按 HTTP 尝试，公网域名优先 HTTPS 后回退 HTTP。
- favicon 发现改为返回绝对候选 URL，使用 `url.ResolveReference` 处理相对路径，并追加 `/favicon.ico`、`/favicon.png`、`/apple-touch-icon.png` fallback。
- 下载逻辑改为受控 GET：设置 5 秒超时、浏览器 User-Agent、2xx 状态校验、`Content-Length` 预检和真实读取字节数硬限制，不再依赖 HEAD。
- `/panel/itemIcon/getSiteFavicon` 会按候选顺序尝试下载，并把更具体的失败原因返回给前端；前端展示后端错误消息。
- 新增 `service/lib/siteFavicon` 单元测试覆盖无协议 host:port、fallback、相对路径解析、HEAD 不支持和真实大小限制。

验证：

- `GOPROXY=https://goproxy.cn,direct go test ./lib/siteFavicon`

改动：

- URL 自动补协议。
- 使用 `url.ResolveReference` 正确解析相对图标地址。
- 增加 `/favicon.ico`、`/favicon.png`、`/apple-touch-icon.png` fallback。
- GET 下载加超时、User-Agent、真实大小限制。
- 不再依赖 HEAD。
- 返回更明确的错误信息。

验收标准：

- 输入 `192.168.1.20:5000` 可以自动尝试获取图标。
- 没有 `<link rel="icon">` 但存在 `/favicon.ico` 的服务可以成功。
- HEAD 不支持但 GET 正常的服务可以成功。
- 超时目标不会卡住接口超过配置时间。

### 第二阶段：安全策略和 Docker 场景提示

目标：在允许家庭内网的同时控制 SSRF 风险。

状态：已完成（2026-05-23）。

完成记录：

- 新增 favicon 抓取选项：超时、最大下载字节数、最大重定向次数、是否允许内网、允许 CIDR、拒绝 host。
- 默认允许 `192.168.0.0/16`、`10.0.0.0/8`、`172.16.0.0/12`，默认拒绝 `localhost`、`127.0.0.1`、`::1`、`169.254.169.254`、unspecified、multicast、link-local 等地址。
- 域名会解析到 IP 后再校验；重定向目标也会重复安全校验，并限制最大重定向次数。
- `localhost` / loopback 返回 Docker 场景提示：容器中的 localhost 不是电脑或 NAS 宿主机。
- 新增 `[favicon]` 默认配置和 `conf.example.ini` 示例配置；API 从配置读取安全和下载参数。
- 新增单元测试覆盖私有 LAN 默认允许、localhost Docker 提示、重定向到禁止 host 被拒绝。

验证：

- `GOPROXY=https://goproxy.cn,direct go test ./lib/siteFavicon`
- `GOPROXY=https://goproxy.cn,direct go test ./api/api_v1/panel` 已通过；验证时临时补充本地 `service/assets/bindata.go` 测试桩以替代仓库中未提交的生成资源包，验证后已删除测试桩。

改动：

- 增加协议校验，只允许 HTTP/HTTPS。
- 增加 IP/CIDR 校验。
- 重定向后重复校验。
- 对 `localhost`、`127.0.0.1`、`169.254.169.254` 返回明确错误。
- 增加 `[favicon]` 配置项。

验收标准：

- 内网地址 `192.168.x.x` 默认允许。
- `127.0.0.1` 默认拒绝，并提示 Docker localhost 含义。
- 重定向到禁止地址会被拒绝。
- 配置项能调整允许 CIDR 和超时时间。

### 第三阶段：缓存与去重

目标：减少重复文件和长期膨胀。

状态：已完成（2026-05-23）。

完成记录：

- 新增 `FaviconCache` 模型并加入数据库 AutoMigrate，记录 `source_url`、`final_icon_url`、`local_src`、`file_id`、`content_hash`、`content_type`、`size`、`last_used_at`。
- `/panel/itemIcon/getSiteFavicon` 下载前按最终图标 URL 复用本地缓存；命中时更新 `last_used_at` 并直接返回本地路径。
- 下载后计算 SHA-256 和大小；若已有相同内容缓存，删除新下载文件并复用旧本地文件，同时为新的最终图标 URL 写入缓存别名。
- 新下载图标会写入 `files` 表和 favicon cache，后续重复获取不再生成多份相同图标。
- 新增 `CleanupUnused` 模型方法，可按 `last_used_at` 阈值清理未被书签 `IconJson` 引用的缓存和本地文件。
- 新增模型单元测试覆盖 URL 复用、内容 hash 复用、`last_used_at` 更新和未引用旧缓存清理。

验证：

- `GOPROXY=https://goproxy.cn,direct go test ./models ./lib/siteFavicon`

改动：

- 增加 favicon cache 表或等价缓存机制。
- 按最终图标 URL 查缓存。
- 下载后计算内容 hash 去重。
- 复用已有本地文件。
- 增加清理策略。

验收标准：

- 同一站点重复获取不会生成多份相同图标。
- 用户取消保存书签不会无限产生孤儿文件。
- 删除书签后，未被引用且长时间未使用的图标可被清理。

### 第四阶段：候选图标选择

目标：提升图标质量。

状态：已完成（2026-05-23）。

完成记录：

- favicon 发现从纯 URL 列表升级为结构化候选，记录 URL、rel、type、sizes、source、purpose 和原始顺序。
- 支持解析 `manifest.webmanifest` / `manifest.json` 中的 `icons`，并按 manifest URL 正确解析相对 `src`。
- 候选排序会综合类型、尺寸、来源和 purpose：优先 SVG；PNG/WebP 优先于 ICO/JPG；`apple-touch-icon` 和 manifest 高清图标加权；仅 `maskable` purpose 降权；fallback 保留 `/favicon.ico`、`/favicon.png`、`/apple-touch-icon.png` 的指定顺序。
- 默认 API 仍保持单击自动选择最佳图标，不新增前端候选选择界面。
- 新增单元测试覆盖 manifest 图标、apple-touch-icon 优先于低清 ICO、SVG 优先于 PNG。

验证：

- `GOPROXY=https://goproxy.cn,direct go test ./lib/siteFavicon ./models`

改动：

- 支持 manifest icons。
- 按 type、sizes、来源排序。
- 可选返回多个候选给前端选择。

验收标准：

- 支持常见 PWA 应用图标。
- 优先选高清 PNG/SVG，而不是低清 `favicon.ico`。
- 仍保持单击获取的默认体验。

### 最终验证记录

状态：已完成（2026-05-23）。

后端验证：

- `GOPROXY=https://goproxy.cn,direct go test -count=1 ./lib/siteFavicon ./models`
- `GOPROXY=https://goproxy.cn,direct go test ./api/api_v1/panel`
- `GOPROXY=https://goproxy.cn,direct go test ./...`

说明：

- 由于仓库中的 `service/assets/bindata.go` 是生成文件且被 `.gitignore` 忽略，完整后端测试时临时创建了本地 `assets.Asset` 测试桩；测试通过后已删除该测试桩。
- 前端依赖未安装成功：当前 `pnpm-lock.yaml` 与本机 pnpm 10 不兼容，`package-lock.json` 又与 `package.json` 不同步；未使用 `--force` 或 `npm install` 重写锁文件，因此未跑前端 type-check。
- GitNexus `detect_changes(scope=all)` 返回 high，主要来自第三阶段新增 `FaviconCache` 并登记到 `CreateDatabase` 的数据库初始化迁移；这是缓存表新增带来的预期影响。

## 推荐优先级

如果只做一轮优化，建议先做第一阶段和第二阶段的核心项：

1. URL 自动补全。
2. favicon fallback。
3. GET 受控下载替代 HEAD。
4. 超时和大小限制。
5. 家庭内网友好的安全校验。
6. 更明确的错误提示。

缓存去重可以放到后续，因为它涉及数据结构和清理策略，但长期部署在 NAS 上时最终应当补上。

## 测试建议

建议准备以下测试目标：

- 公网站点：`https://github.com`
- 只有 `/favicon.ico` 的简单服务
- 不支持 HEAD 但支持 GET 的测试服务
- 局域网 IP：`http://192.168.x.x:port`
- 局域网域名：`http://nas.local:port`
- 无协议输入：`192.168.x.x:port`
- 禁止地址：`http://127.0.0.1:port`
- 重定向到禁止地址的测试服务
- 超大图标文件
- 非图片响应

后端单元测试重点：

- URL 规范化。
- CIDR 安全校验。
- 相对 URL 解析。
- 候选排序。
- 下载大小限制。
- 错误分类。

手工验收重点：

- Docker 容器中能访问 NAS 局域网服务。
- 获取成功后刷新页面仍显示本地图标。
- 重建容器后，volume 中的图标不丢失。

## 不建议的方案

### 不建议前端直接读取远程 favicon

原因：

- 浏览器会受 CORS 限制。
- 面板显示依赖远程服务在线状态。
- 内网证书、自签名 HTTPS、混合内容会增加失败率。
- 无法统一缓存和清理。

### 不建议简单禁止所有私有 IP

原因：

- 这会直接破坏家庭 NAS 场景。
- 你的书签目标大概率就是私有 IP 或 `.local/.lan` 域名。

### 不建议每次都强制重新下载

原因：

- 长期部署会积累大量重复文件。
- NAS 存储虽然充足，但无意义文件会影响管理和备份。

## 最终建议

采用“服务端受控抓取 + 家庭内网允许策略 + 多级 favicon fallback + 本地缓存”的方案。

这条路线最符合 NAS + Docker 部署方式：图标一旦抓取成功就保存在本地，面板加载稳定；同时通过 URL 规范化、超时、大小限制、CIDR 校验解决局域网可用性和安全性之间的冲突。
