# go-magic Scoop Manifest

该文件属于 bucket 仓库 `magicwubiao/scoop-bucket`（当前仓库只是源文件，bucket 仓库尚未创建，见下方 README 说明）。

## 用法（bucket 发布之后）

```powershell
scoop bucket add magic https://github.com/magicwubiao/scoop-bucket
scoop install magic
```

## 说明

- 资产名与 CI 一致：`go-magic-windows-amd64.exe` / `go-magic-windows-arm64.exe`
  （CI 只发布 amd64 与 arm64，没有 386）
- `hash` 使用 GitHub Release 上的 sha256 摘要；`autoupdate` 通过 `$version` 自动拼接新 URL
- `bin` 用 `[["源文件", "别名"]]` 形式把 shim 命名成 `magic`，
  因此**不需要** post_install 里再重命名文件
- 本文件必须保持为**严格 JSON**（不能有 `#` 注释，Scoop 用 `ConvertFrom-Json` 解析）
