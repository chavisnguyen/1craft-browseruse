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
      url "https://github.com/chavisnguyen/klik/releases/download/v2.0.1/klik-darwin-arm64.tar.gz"
      sha256 "e80d3a79c4c70ff858a23e683469a68c8f00f4aecd21253a3254866e289128c7"
    end
    on_intel do
      url "https://github.com/chavisnguyen/klik/releases/download/v2.0.1/klik-darwin-amd64.tar.gz"
      sha256 "101469e7fd64a64942cd15f53abec869375980c8ee524fd21cfb13ceb4d302db"
    end
  end

  on_linux do
    # Prebuilt Linux binaries are x86_64 only.
    depends_on arch: :x86_64
    url "https://github.com/chavisnguyen/klik/releases/download/v2.0.1/klik-linux-amd64.tar.gz"
    sha256 "38ced5e4f776cda9b2baf6ca99f260a27dc4ebed3d4ca694f497e43905f2a54c"
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
