class Openbitdo < Formula
  desc "Beginner-first clean-room 8BitDo TUI utility"
  homepage "https://github.com/bybrooklyn/openbitdo"
  license "BSD-3-Clause"
  version "0.0.1-rc.1"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/bybrooklyn/openbitdo/releases/download/v0.0.1-rc.1/openbitdo-v0.0.1-rc.1-macos-arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  on_linux do
    if Hardware::CPU.intel?
      url "https://github.com/bybrooklyn/openbitdo/releases/download/v0.0.1-rc.1/openbitdo-v0.0.1-rc.1-linux-x86_64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    elsif Hardware::CPU.arm?
      url "https://github.com/bybrooklyn/openbitdo/releases/download/v0.0.1-rc.1/openbitdo-v0.0.1-rc.1-linux-aarch64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  # Release automation rewrites checksums in the tap with authoritative values
  # from published assets.
  def install
    bin.install "bin/openbitdo"
  end

  test do
    assert_match "openbitdo", shell_output("#{bin}/openbitdo --help")
  end
end
