# =============================================================================
# go-magic Homebrew Formula
# =============================================================================
# This file belongs in Formula/ of the tap repository magicwubiao/homebrew-tap
# (this repository only holds the source file; the tap repository has not been
# created yet, see README.md in the same directory).
#
# Asset names match CI exactly (.github/workflows/release.yml uploads **bare
# binaries**, there is no tar.gz):
#   go-magic-darwin-amd64 / go-magic-darwin-arm64
#   go-magic-linux-amd64  / go-magic-linux-arm64
#
# version and sha256 must correspond to a published Release; the tap automation
# (brew bump) updates them when a new version ships. The current values
# correspond to Release v0.5.19.
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
    # The download is a bare binary whose file name varies per platform, so match
    # by prefix and rename it to magic
    binary = Dir["go-magic-*"].first
    odie "no go-magic binary found in the download" if binary.nil?
    bin.install binary => "magic"
  end

  def caveats
    <<~EOS
      Initialize the configuration before first use:
        magic setup
        magic chat
        magic server

      The config directory is ~/.magic (independent of where the binary lives).
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
