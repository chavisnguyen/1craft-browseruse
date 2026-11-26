class Klik < Formula
  desc "Control your browser with your AI - Ultra-lightweight (~12MB RAM) MCP server"
  homepage "https://github.com/chavisnguyen/klik"
  license "MIT"

  # NOTE: `url`/`sha256` below are rewritten on every published Release by
  # the "Release assets" workflow (.github/workflows/release.yml). Do not edit
  # by hand. (`version` is inferred from the tag in the URL.)
  livecheck do
    url :homepage
    strategy :github_latest
  end

  on_macos do
    on_arm do
      url "https://github.com/chavisnguyen/klik/releases/download/v3.2.0/klik-darwin-arm64.tar.gz"
      sha256 "26f38733d9322e3da65af6298d358b360946391e0315a499df743fd675f1cb26"
    end
    on_intel do
      url "https://github.com/chavisnguyen/klik/releases/download/v3.2.0/klik-darwin-amd64.tar.gz"
      sha256 "96accc10393269879b0e30f992094b8a5c7dc306e1bfd0636e528cd33ef6492f"
    end
  end

  on_linux do
    # Prebuilt Linux binaries are x86_64 only.
    depends_on arch: :x86_64
    url "https://github.com/chavisnguyen/klik/releases/download/v3.2.0/klik-linux-amd64.tar.gz"
    sha256 "e4aa5303bee046b000083fe31177432e013675b438a78a3a0c51d1c62315103e"
  end

  def install
    bin.install "klik"
    pkgshare.install "extension"
  end

  service do
    run [opt_bin/"klik", "-sse", "-port", "3025"]
    keep_alive true
    log_path var/"log/klik.log"
    error_log_path var/"log/klik.log"
  end

  def caveats
    <<~EOS
      Quickstart:
        1. Start the background daemon:
             brew services start klik

        2. One-command setup (token + extension + register MCP in your agents):
             klik setup

        3. Load the Chrome extension (chrome://extensions -> Developer mode -> Load unpacked):
             ~/.klik/extension

        4. Restart your AI agent. Problems? Run: klik doctor
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/klik --version")
    assert_predicate pkgshare/"extension/manifest.json", :exist?
  end
end
