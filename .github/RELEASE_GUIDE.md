# GitHub Actions 自动构建指南

## 手动创建 Release 的步骤

### 1. 本地创建并推送 Tag

```bash
git tag v1.0.0
git push origin v1.0.0
```

### 2. GitHub 将自动

1. 运行 CI 构建所有平台
2. 生成 SHA256 校验和
3. 创建 Release 并上传文件

### 3. 下载 Release

访问: https://github.com/magicwubiao/go-magic/releases

## 支持的平台

| 平台 | 架构 | 状态 |
|------|------|------|
| Linux | amd64, arm64 | ✅ |
| macOS | amd64, arm64 | ✅ |
| Windows | amd64 | ✅ |

## 本地测试构建

```powershell
# Windows
go build -ldflags="-s -w" -o magic.exe ./cmd/magic
```

## 修复 Token 权限

如果 GitHub Actions 被拒绝，推送 workflow 文件：

1. 在 GitHub -> Settings -> Developer settings -> Personal access tokens
2. 生成新 token，勾选 `workflow` 权限
3. 更新本地 git remote 使用 token:

```bash
git remote set-url origin https://YOUR_TOKEN@github.com/magicwubiao/go-magic.git
git push origin main
```
