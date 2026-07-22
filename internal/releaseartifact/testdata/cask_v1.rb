# Immutable ecctl updater Cask v1 compatibility fixture.
cask "ecctl" do
  version "1.2.3"
  on_macos do
    on_intel do
      sha256 "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      url "https://ros-public-tools.oss-cn-beijing.aliyuncs.com/github-releases/aliyun/elastic-compute-control-cli/#{version}/ecctl_#{version}_darwin_amd64.tar.gz",
        verified: "ros-public-tools.oss-cn-beijing.aliyuncs.com/github-releases/aliyun/elastic-compute-control-cli/"
    end
    on_arm do
      sha256 "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      url "https://ros-public-tools.oss-cn-beijing.aliyuncs.com/github-releases/aliyun/elastic-compute-control-cli/#{version}/ecctl_#{version}_darwin_arm64.tar.gz",
        verified: "ros-public-tools.oss-cn-beijing.aliyuncs.com/github-releases/aliyun/elastic-compute-control-cli/"
    end
  end
  name "ecctl"
  desc "Agent-first command-line controller for Alibaba Cloud elastic computing resources"
  homepage "https://github.com/aliyun/elastic-compute-control-cli"
  livecheck do
    skip "Auto-generated on release."
  end
  binary "ecctl"
  postflight do
    system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/ecctl"]
  end
end
