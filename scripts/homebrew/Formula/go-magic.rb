class GoMagic < Formula
  desc "High-performance AI Agent in Go"
  homepage "https://github.com/magicwubiao/go-magic"
  version "dev"
  license "MIT"

  if OS.mac?
    if Hardware::CPU.intel?
      url "https://github.com/magicwubiao/go-magic/releases/download/v#{version}/magic-darwin-amd64.tar.gz"
      sha256 "update_with_actual_checksum"
    elsif Hardware::CPU.arm?
      url "https://github.com/magicwubiao/go-magic/releases/download/v#{version}/magic-darwin-arm64.tar.gz"
      sha256 "update_with_actual_checksum"
    end
  elsif OS.linux?
    if Hardware::CPU.intel?
      url "https://github.com/magicwubiao/go-magic/releases/download/v#{version}/magic-linux-amd64.tar.gz"
      sha256 "update_with_actual_checksum"
    elsif Hardware::CPU.arm?
      url "https://github.com/magicwubiao/go-magic/releases/download/v#{version}/magic-linux-arm64.tar.gz"
      sha256 "update_with_actual_checksum"
    end
  end

  def install
    # Install binary
    bin.install "magic"
    
    # Create config directory
    (prefix/"go-magic").mkpath
  end

  def post_install
    puts ""
    puts "go-magic has been installed!"
    puts ""
    puts "Usage:"
    puts "  magic --help              # Show help"
    puts "  magic chat               # Start interactive chat"
    puts "  magic setup              # Initial setup"
    puts ""
    puts "Configuration is stored in: #{Dir.home}/.magic"
    puts ""
  end

  test do
    system "#{bin}/magic --version"
  end
end
