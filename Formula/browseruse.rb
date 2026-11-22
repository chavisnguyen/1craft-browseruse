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
      url "https://github.com/chavisnguyen/1craft-browseruse/releases/download/v2.0.0/1craft-browseruse-darwin-arm64.tar.gz"
      sha256 "a6bfafd4d252e860a45e0fe11a2a666eeeb827bf8209a8ed2f6e932c44a3c59d"
    end
    on_intel do
      url "https://github.com/chavisnguyen/1craft-browseruse/releases/download/v2.0.0/1craft-browseruse-darwin-amd64.tar.gz"
      sha256 "8bc43dbb44c3e90ae9b64d7c755d376639597520faa730dcfa0936bfa7d1a957"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/chavisnguyen/1craft-browseruse/releases/download/v2.0.0/1craft-browseruse-linux-amd64.tar.gz"
      sha256 "b74b2a052f2eb1507ce0e26ca92ab22e302b946d1dd478aa24bd147bcdfddcdc"
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
