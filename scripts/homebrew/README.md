# go-magic Homebrew Formula

`Formula/go-magic.rb` 属于 tap 仓库 `magicwubiao/homebrew-tap`；本仓库只是公式的源文件。

## 现状（重要）

- tap 仓库 `magicwubiao/homebrew-tap` **目前尚未创建**，因此 `scripts/install.sh --method homebrew`
  与 `scripts/install-homebrew.sh --tap` 暂时无法成功，会给出明确提示。
- 未创建之前，请使用 `scripts/install.sh`（默认 binary 方式）或
  `scripts/install-homebrew.sh --prefix <brew-prefix>`（直接把二进制装到 `<prefix>/bin`）。

## 发布 tap 之后的使用方式

```bash
brew tap magicwubiao/tap
brew install magicwubiao/tap/go-magic
```

或：

```bash
scripts/install.sh --method homebrew
```

## 维护规则

1. **资产名必须与 CI 一致**：`.github/workflows/release.yml` 上传的是裸二进制
   `go-magic-<os>-<arch>`（Windows 为 `.exe`），**没有** `.tar.gz` / `.zip`，
   此前公式指向不存在的 `magic-*.tar.gz` 属于错误。
2. **版本与 sha256 必须同属一个 Release**：
   取 GitHub API 的 `assets[].digest` 或 Release 页面校验和，例如：

   ```bash
   curl -fsSL https://api.github.com/repos/magicwubiao/go-magic/releases/latest \
     | grep -E '"name"|"digest"'
   ```

3. 发新版时更新 `version` 与四个 `sha256`（darwin/linux × amd64/arm64），
   或将本文件设为 tap 仓库的自动化输入（brew bump / 手动 PR）。
4. `test do` 依赖 `magic --version`（该参数由 `cmd/magic/main.go` 显式支持），不要改成 `magic version` 以外的自定义输出。
