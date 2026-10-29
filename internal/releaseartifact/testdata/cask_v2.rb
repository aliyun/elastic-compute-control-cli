# ecctl Homebrew Cask v2 structured install steps.
cask "ecctl" do
  version "1.2.3"
  on_macos do
    on_arm do
      sha256 "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      url "https://ros-public-tools.oss-cn-beijing.aliyuncs.com/github-releases/aliyun/elastic-compute-control-cli/#{version}/ecctl_#{version}_darwin_arm64.tar.gz"
    end
    on_intel do
      sha256 "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      url "https://ros-public-tools.oss-cn-beijing.aliyuncs.com/github-releases/aliyun/elastic-compute-control-cli/#{version}/ecctl_#{version}_darwin_amd64.tar.gz"
    end
  end
  name "ecctl"
  desc "Agent-first command-line controller for Alibaba Cloud elastic computing resources"
  homepage "https://github.com/aliyun/elastic-compute-control-cli"
  livecheck do
    skip "Auto-generated on release."
  end
  depends_on :macos
  binary "ecctl"
  postflight_steps do
    on_macos do
      run "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "{{staged_path}}/ecctl"]
    end
  end
end
