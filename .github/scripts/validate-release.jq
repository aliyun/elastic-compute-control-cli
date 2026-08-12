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

. as $release |
($release.tag_name == $tag) and
($release.draft == $draft) and
($release.immutable == $immutable) and
($release.prerelease == ($version | contains("-"))) and
($release.html_url | type == "string") and
(if $draft then
  ($release.html_url | startswith("https://github.com/\($repository)/releases/tag/untagged-")) and
  (($release.html_url | ltrimstr("https://github.com/\($repository)/releases/tag/untagged-")) | test("^[0-9a-f]+$"))
else
  ($release.html_url == "https://github.com/\($repository)/releases/tag/\($tag)")
end) and
($release.assets | type == "array") and
(($release.assets | length) == (expected_assets | length)) and
(([$release.assets[].name] | sort) == (expected_assets | sort)) and
all($release.assets[]; . as $asset |
  ($asset.name | type == "string") and
  ($asset.state == "uploaded") and
  ($asset.digest | type == "string") and
  ($asset.digest | test("^sha256:[0-9a-f]{64}$")) and
  ($asset.browser_download_url | type == "string") and
  ($asset.browser_download_url == (($release.html_url | sub("/releases/tag/"; "/releases/download/")) + "/" + $asset.name))
)
