cask "ecctl" do
  version "0.2.5"
  on_macos do
    on_arm do
      sha256 "5800fd3b3f7283d6f908692b9debad83c06550e082ed4070ec69ba216c4adb63"
      url "https://ros-public-tools.oss-cn-beijing.aliyuncs.com/github-releases/aliyun/elastic-compute-control-cli/#{version}/ecctl_#{version}_darwin_arm64.tar.gz"
    end
    on_intel do
      sha256 "ee46ef0d20e5b57a4189724c6b4d9f17f02e8237091add6020d6896db152ef3b"
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
