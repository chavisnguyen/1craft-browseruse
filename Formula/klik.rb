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
      url "https://github.com/chavisnguyen/klik/releases/download/v3.1.0/klik-darwin-arm64.tar.gz"
      sha256 "d7fedf3dde0a3a351397a34da0ceab496eb8aceb974919cc90d8c209cdeb1868"
    end
    on_intel do
      url "https://github.com/chavisnguyen/klik/releases/download/v3.1.0/klik-darwin-amd64.tar.gz"
      sha256 "eda5567e94bb1b220fd466b9c2b656546511926d59a811c4e305cfed083612cf"
    end
  end

  on_linux do
    # Prebuilt Linux binaries are x86_64 only.
    depends_on arch: :x86_64
    url "https://github.com/chavisnguyen/klik/releases/download/v3.1.0/klik-linux-amd64.tar.gz"
    sha256 "f686fcf1b1cd7e5a79ec89e4b8677360353638d8a9d36e209248e561f3309eb7"
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
