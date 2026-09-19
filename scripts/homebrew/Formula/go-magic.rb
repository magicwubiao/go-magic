# =============================================================================
# go-magic Homebrew Formula
# =============================================================================
# 该文件属于 tap 仓库 magicwubiao/homebrew-tap 的 Formula/ 目录（当前仓库只是源文件，
# tap 仓库尚未创建，见同目录 README.md）。
#
# 资产名与 CI 完全一致（.github/workflows/release.yml 上传的是**裸二进制**，没有 tar.gz）：
#   go-magic-darwin-amd64 / go-magic-darwin-arm64
#   go-magic-linux-amd64  / go-magic-linux-arm64
#
# version 与 sha256 必须与某个已发布 Release 对应；发新版时由 tap 的自动化（brew bump）更新。
# 当前值对应 Release v0.5.19。
# =============================================================================
class GoMagic < Formula
  desc "High-performance AI Agent in Go"
  homepage "https://github.com/magicwubiao/go-magic"
  version "0.5.19"
  license "MIT"

  on_macos do
    on_intel do
      url "https://github.com/magicwubiao/go-magic/releases/download/v#{version}/go-magic-darwin-amd64"
      sha256 "c013d74a0e0fd36db0afee2d458dcfda3ccc817c8a896620747e505afd0dc2e6"
    end
    on_arm do
      url "https://github.com/magicwubiao/go-magic/releases/download/v#{version}/go-magic-darwin-arm64"
      sha256 "67ae44184c23879a09dbab4f0360f9d0b37a84a7356042bf03d5a73f7264d888"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/magicwubiao/go-magic/releases/download/v#{version}/go-magic-linux-amd64"
      sha256 "d4947c349c4aaa1cd888cdfef023edf4ec7b51619766ba969c66f6f6249cbfe1"
    end
    on_arm do
      url "https://github.com/magicwubiao/go-magic/releases/download/v#{version}/go-magic-linux-arm64"
      sha256 "439c299745a34527f381f9913a47093572b70326934ff8a65f2c83a3e9b4f6fe"
    end
  end

  def install
    # 下载的是裸二进制，文件名随平台变化，因此按前缀匹配后重命名为 magic
    binary = Dir["go-magic-*"].first
    odie "无法在下载内容中找到 go-magic 二进制" if binary.nil?
    bin.install binary => "magic"
  end

  def caveats
    <<~EOS
      首次使用请先初始化配置：
        magic setup
        magic chat
        magic server

      配置目录为 ~/.magic（与二进制安装位置无关）。
    EOS
  end

  livecheck do
    url :stable
    regex(/^v?(\d+(?:\.\d+)+)$/i)
    strategy :github_latest
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/magic --version")
  end
end
