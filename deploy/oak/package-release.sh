#!/usr/bin/env bash
# 将已构建的二进制与必要许可文件组成可校验的发布包。
# 此脚本不接触服务器，也不读取任何部署凭据或持久数据。
# 调用方负责构建源码；CI 使用 github.sha 保证提交和产物对应。
set -euo pipefail
umask 077

# 四个参数均由 CI 显式提供，不从外部文件猜测发布版本。
if [[ $# != 4 ]]; then
  echo '用法：package-release.sh <二进制> <发布标识> <提交SHA> <输出目录>' >&2
  exit 2
fi
binary=$1
release=$2
revision=$3
output=$4
# 发布目录名不能包含空格、路径或 Shell 元字符。
# SHA 前缀必须与完整提交相同，避免清单和目录标识混淆。
[[ $release =~ ^oak-[0-9a-f]{12}-[0-9]+-[0-9]+$ ]]
[[ $revision =~ ^[0-9a-f]{40}$ && $release == oak-"${revision:0:12}"-* ]]
[[ -f $binary && ! -L $binary ]]
# 从脚本自身位置定位仓库，调用方无需事先切换目录。
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# 每次打包使用独立暂存，避免夹带上一次构建的文件。
staging=$(mktemp -d)
trap 'rm -rf -- "$staging"' EXIT
mkdir -p -- "$output"
output=$(cd -- "$output" && pwd)
# 固定文件清单同时保留项目及第三方许可证声明。
cp -- "$binary" "$staging/new-api"
cp -- "$repo/LICENSE" "$repo/NOTICE" "$repo/THIRD-PARTY-LICENSES.md" "$staging/"
chmod 755 "$staging/new-api"
printf '%s\n' "$release" > "$staging/VERSION"
printf '%s\n' "$revision" > "$staging/REVISION"
# 固定 USTAR 格式，不生成 PAX、长名称或 macOS 资源叉扩展头。
# 不使用通配符，新增工作区文件不会意外进入发布包。
COPYFILE_DISABLE=1 tar --format=ustar -czf "$output/release.tar.gz" -C "$staging" new-api LICENSE NOTICE THIRD-PARTY-LICENSES.md VERSION REVISION
# 远端和本地测试复用相同的包校验，不维护第二套格式规则。
digest=$(shasum -a 256 "$output/release.tar.gz")
digest=${digest%% *}
bash "$repo/deploy/oak/ci-deploy.sh" --validate-package "$output/release.tar.gz" "$release" "$revision" "$digest"
# 校验文件只记录相对包名，GitHub 下载到不同目录后仍可核对。
printf '%s  release.tar.gz\n' "$digest" > "$output/release.tar.gz.sha256"
printf '已生成发布包：%s/release.tar.gz\n' "$output"
