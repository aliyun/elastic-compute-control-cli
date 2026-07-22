def expected_assets:
  [
    "checksums.txt",
    "version.txt",
    "ecctl_\($version)_darwin_amd64.tar.gz",
    "ecctl_\($version)_darwin_arm64.tar.gz",
    "ecctl_\($version)_linux_amd64.tar.gz",
    "ecctl_\($version)_linux_arm64.tar.gz",
    "ecctl_\($version)_windows_amd64.zip",
    "ecctl_\($version)_windows_arm64.zip"
  ] + (if ($version | contains("-")) then [] else ["ecctl_\($version)_cask.rb"] end);

(.tag_name == $tag) and
(.draft == $draft) and
(.immutable == $immutable) and
(.prerelease == ($version | contains("-"))) and
(.assets | type == "array") and
((.assets | length) == (expected_assets | length)) and
(([.assets[].name] | sort) == (expected_assets | sort)) and
all(.assets[];
  (.name | type == "string") and
  (.state == "uploaded") and
  (.digest | type == "string") and
  (.digest | test("^sha256:[0-9a-f]{64}$")) and
  (.browser_download_url | type == "string") and
  (.browser_download_url | startswith("https://github.com/\($repository)/releases/download/\($tag)/"))
)
