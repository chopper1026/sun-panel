## 静态资源内嵌

本目录中的配置、语言包和版本文件通过 Go 标准库 `embed` 打包到后端程序中。

本地开发不再需要安装或运行 `go-bindata`：

```bash
cd service
go run main.go
```

如果本地残留旧的 `service/assets/bindata.go`，请删除它，避免和 `embed.go` 中的 `Asset` 函数重复定义。
