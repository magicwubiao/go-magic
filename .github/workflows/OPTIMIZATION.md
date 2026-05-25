# GitHub Actions 工作流优化总结

## 问题修复

### 1. Release 403 错误（已修复）
**问题**: `softprops/action-gh-release@v1` 返回 403 权限错误
**原因**: 缺少 `contents: write` 权限
**修复**: 在 release.yml 顶部添加：
```yaml
permissions:
  contents: write
```

### 2. DEB 包路径不匹配（已修复）
**问题**: `artifacts/*.deb` 找不到文件
**原因**: DEB 包在 `artifacts/go-magic-deb-package/` 子目录中
**修复**: 使用 `merge-multiple: true` 合并所有 artifact 到同一目录

### 3. 版本号硬编码（已修复）
**问题**: fpm 命令中版本号写死为 `1.0.0`
**修复**: 使用 `${VERSION#v}` 动态获取版本号（去掉 v 前缀）

## 一致性改进

| 项目 | 之前 | 之后 |
|------|------|------|
| 构建矩阵 | CI 用 include，Release 用 exclude | 统一使用 include |
| npm install | 使用 `npm install` | 使用 `npm ci`（更快、确定性更强） |
| Node 缓存 | 无缓存 | 添加 `cache: 'npm'` |
| 权限声明 | 无 | CI: `contents: read`, Release: `contents: write` |
| 版本注入 | 无 | 使用 `-X main.Version=$VERSION` |

## 功能增强

### CI 新增
- **Go 代码格式化检查**: 使用 `gofmt` 确保代码格式一致
- **产物保留策略**: CI 产物保留 7 天

### Release 新增
- **独立的 get-version job**: 版本号计算一次，多处使用
- **lint-and-test job**: Release 也运行测试，确保质量
- **自动生成 Release Notes**: 使用 `generate_release_notes: true`
- **升级 action-gh-release**: v1 → v2

## 依赖关系

```
get-version
    ↓
lint-and-test ─────────┬────────┐
    ↓                  ↓        ↓
build-cross      build-source  build-linux-packages
                          ↓
                    create-release
```

## 使用建议

1. **确保 `package-lock.json` 已提交**: `npm ci` 需要它
2. **检查 GitHub Token 权限**: Settings → Actions → General → Workflow permissions → 勾选 "Read and write permissions"
3. **测试发布流程**: 先推送 `v0.0.0-test` 标签验证
