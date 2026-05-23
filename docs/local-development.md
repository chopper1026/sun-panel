# 本地开发启动

推荐使用一键脚本：

```bash
make dev
```

等价命令：

```bash
./scripts/dev.sh
```

脚本会自动完成：

- 使用 pnpm 8.15.9 按 `pnpm-lock.yaml` 安装前端依赖。
- 启动后端 `service/main.go`，默认地址 `http://127.0.0.1:3002`。
- 启动前端 Vite，默认地址 `http://127.0.0.1:1002`。
- 退出时同时停止后端和前端进程。

常用命令：

```bash
make backend      # 只启动后端
make frontend     # 只启动前端
make test-backend # 运行后端测试
```

说明：

- 后端已使用 Go 标准库 `embed` 内嵌 `service/assets` 资源，本地开发不再需要安装或运行 `go-bindata`。
- 如果本地残留旧的 `service/assets/bindata.go`，`scripts/dev.sh` 会自动删除，避免和 `embed.go` 重复定义。
- 前端 API 代理配置在 `.env`，默认 `VITE_APP_API_BASE_URL=http://127.0.0.1:3002/`。
