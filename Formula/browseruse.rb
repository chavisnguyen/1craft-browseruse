class Browseruse < Formula
  desc "Control your browser with your AI - Ultra-lightweight (~12MB RAM) MCP server"
  homepage "https://github.com/chavisnguyen/1craft-browseruse"
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
      url "https://github.com/chavisnguyen/1craft-browseruse/releases/download/v1.3.0/1craft-browseruse-darwin-arm64.tar.gz"
      sha256 "50e6c363c82068333826a90cc03abac68b640e9cba00ebff8926365168ac714d"
    end
    on_intel do
      url "https://github.com/chavisnguyen/1craft-browseruse/releases/download/v1.3.0/1craft-browseruse-darwin-amd64.tar.gz"
      sha256 "74721ce860dfad8edc985502921744f5f6a54fd152ec015128aa6d9c3d2fc18b"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/chavisnguyen/1craft-browseruse/releases/download/v1.3.0/1craft-browseruse-linux-amd64.tar.gz"
      sha256 "8be9ca1e8931324175a9625e4985cfc1876002b7ad0e3506fafca4c7b2bd7e46"
    end
  end

  def install
    bin.install "1craft-browseruse"
    bin.install_symlink "1craft-browseruse" => "browseruse"
    pkgshare.install "extension"
  end

  service do
    run [opt_bin/"1craft-browseruse", "-sse", "-port", "3025"]
    keep_alive true
    log_path var/"log/1craft-browseruse.log"
    error_log_path var/"log/1craft-browseruse.log"
  end

  def caveats
    <<~EOS
      Quickstart:
        1. Start the background daemon:
             brew services start browseruse

        2. One-command setup (token + extension + register MCP in your agents):
             browseruse setup

        3. Load the Chrome extension (chrome://extensions -> Developer mode -> Load unpacked):
             ~/.1craft-browseruse/extension

        4. Restart your AI agent. Problems? Run: browseruse doctor
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/1craft-browseruse --version")
    assert_predicate pkgshare/"extension/manifest.json", :exist?
  end
end
