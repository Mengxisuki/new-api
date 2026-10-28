#!/usr/bin/env bash
# Oak 原生发布入口；生产路径、服务名和域名故意固定，不能由 SSH 参数覆盖。
# 此文件由 root 安装；受限部署密钥只能调用 check 或 deploy 三参数命令。
# 本地 --validate-package 只核验文件，不需要 root、不解压、不执行二进制。
set -euo pipefail
umask 077 # 暂存和备份默认仅 root 可访问，不让凭据进入 CI 产物。
export PATH=/usr/sbin:/usr/bin:/sbin:/bin # 受限入口只使用系统工具，不接受远端 PATH 注入。

readonly RELEASES=/opt/new-api/releases # 发布目录只允许脚本生成的单层标识
readonly CURRENT=/opt/new-api/current # 原子切换此链接，不覆盖已运行的文件
readonly BACKUPS=/opt/new-api/backups # 恢复点保留在服务器，不上传到 GitHub
readonly ENVIRONMENT=/etc/new-api/new-api.env # 只作受限备份，禁止 source 或打印配置
readonly STATE=/var/lib/new-api # 数据归属独立于发布，不在切换时替换
readonly PUBLIC_URL=https://oak.qingshukeji.top # 公网验收与回环检查对应同一部署
readonly MAX_BYTES=536870912 # 同时限制压缩包与全部解压文件大小为 512 MiB。
work='' # 只清理本次 mktemp 创建的目录，不删除发布或备份记录。
backup='' # 早期失败可能还没有备份，错误报告不伪造路径
previous='' # 由 current 的真实目标获得，不接受客户端指定
stopped=0 # 停止旧程序后、启动新程序前，仍可安全恢复旧服务。
start_attempted=0 # 必须在 systemctl start 前置位，失败启动也可能已迁移。

fail() {
  printf '%s\n' "$*" >&2 # 错误可见，调用方不能把失败部署当作成功。
  exit 1
}

validate_identity() {
  # 只接受 CI 生成的标识，禁止路径、Shell 元字符和不对应的提交前缀。
  [[ $1 =~ ^oak-[0-9a-f]{12}-[0-9]+-[0-9]+$ ]] || return 1
  [[ $2 =~ ^[0-9a-f]{40}$ && $1 == oak-"${2:0:12}"-* ]] || return 1
  [[ $3 =~ ^[0-9a-f]{64}$ ]] # 校验和不能携带文件名或额外选项。
}

validate_package() {
  # 所有入口共用同一校验，避免本地通过而服务器接受另一种格式。
  # 第五参数仅内部部署传入；CLI 校验始终传空串，完全不落地归档成员。
  python3 - "$1" "$2" "$3" "$4" "${5:-}" <<'PY'
import gzip
import hashlib
import sys
import tarfile
from contextlib import nullcontext
from pathlib import Path

archive, release, revision, expected, destination = sys.argv[1:] # 参数由 Bash 独立传递，不插入 Python 代码
limit = 512 * 1024 * 1024  # 还要限制解压体积，不能只防范压缩包过大。
required = {'new-api', 'LICENSE', 'NOTICE', 'THIRD-PARTY-LICENSES.md', 'VERSION', 'REVISION'} # 白名单不包含任何路径分隔符
try:
    source = Path(archive) # 压缩包已接收到独立暂存，不从线上目录取输入
    if source.stat().st_size > limit: # 本地校验也执行相同的压缩体积约束
        raise ValueError('压缩包超过 512 MiB')
    digest = hashlib.sha256()
    with source.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)  # 分块读取，校验大文件不占用等量内存。
    if digest.hexdigest() != expected: # 先验证整个包，再解析不可信归档头
        raise ValueError('压缩包 SHA-256 不匹配')
    # 手动逐头迭代：TarFile 会先吞掉 PAX/长名头，导致特殊头绕过成员体积检查。
    # 标准库仍负责头部校验和、数字及名称解析；只支持发布器明确生成的 USTAR。
    with gzip.open(source, 'rb') as package:
        members = set()  # 名称必须唯一，后出现成员不能覆盖前面的文件。
        total = 0  # 此处计入头、成员填充和尾部零块，限制完整解压流。
        while True:
            raw = package.read(512)  # 读取固定长度，不信任输入头声明的扩展体积。
            total += len(raw)
            if total > limit:
                raise ValueError('归档解压流超过 512 MiB')
            if raw == bytes(512):
                # 正常 tar 尾部只有零块；拒绝终止标记后的隐藏成员或其他负载。
                for block in iter(lambda: package.read(1024 * 1024), b''):
                    total += len(block)
                    if total > limit or block.strip(b'\0'):
                        raise ValueError('归档尾部非法或解压流超过 512 MiB')
                break
            if len(raw) != 512 or raw[257:263] != b'ustar\0':
                raise ValueError('归档必须使用完整 USTAR 普通文件头')
            member = tarfile.TarInfo.frombuf(raw, 'utf-8', 'strict')
            if member.name not in required or member.name in members:
                raise ValueError('归档包含非白名单或重复文件')
            if member.type not in (tarfile.REGTYPE, tarfile.AREGTYPE) or member.linkname:
                raise ValueError('归档只允许普通文件，禁止目录、链接和设备')
            padded = (member.size + 511) // 512 * 512  # 成员按 tar 块边界对齐。
            if member.size <= 0 or total + padded > limit:
                raise ValueError('归档文件为空或解压体积超过 512 MiB')
            metadata = {'VERSION': release, 'REVISION': revision}.get(member.name)
            if metadata is not None and member.size != len(metadata) + 1:
                raise ValueError(member.name + ' 长度与目标元数据不匹配')
            target = Path(destination) / member.name if destination else None
            # 部署在完整校验通过后再调用同一解析器提取；本地始终不创建文件。
            with target.open('xb') if target else nullcontext() as output:
                remaining = member.size
                first = True
                while remaining:
                    block = package.read(min(1024 * 1024, remaining))
                    if not block:
                        raise ValueError('归档成员内容被截断')
                    if first and member.name == 'new-api':
                        # 只读平台头，不执行程序；完整可执行性由远端非特权预检确认。
                        if len(block) < 20 or block[:6] != b'\x7fELF\x02\x01' or block[18:20] != b'\x3e\x00':
                            raise ValueError('二进制必须为 Linux amd64 ELF')
                    if metadata is not None and block != (metadata + '\n').encode('ascii'):
                        raise ValueError(member.name + ' 与目标元数据不匹配')
                    if output:
                        output.write(block)  # 固定白名单路径，不恢复归档 UID 或权限。
                    remaining -= len(block)
                    first = False
            padding = padded - member.size
            if package.read(padding) != bytes(padding):
                raise ValueError('归档成员填充无效或被截断')
            total += padded
            members.add(member.name)
            if target:
                target.chmod(0o755 if member.name == 'new-api' else 0o644)
        if members != required:  # 缺少许可证或版本清单同样不能发布。
            raise ValueError('归档缺少必需文件')
except (OSError, ValueError, tarfile.TarError, EOFError, UnicodeError) as error:
    print('发布包校验失败：' + str(error), file=sys.stderr)
    sys.exit(1)
PY
}

binary_version() {
  # 预检以应用用户运行，且去除可覆盖链接版本的 VERSION 环境变量。
  # cd / 后执行本脚本，避免应用顺便读取调用目录内的 .env。
  timeout 15 runuser -u new-api -- env -u VERSION "$1/new-api" --version
}

health() {
  local expected=$1 url=$2
  # 不输出整个状态响应；只判定成功标志与精确版本，日志不夹带配置。
  curl --fail --silent --show-error --max-time 10 "$url/api/status" |
    python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data.get("success") is True and data.get("data", {}).get("version") == sys.argv[1] else 1)' "$expected"
}

database_ready() {
  # 使用 PostgreSQL 本机管理员身份，不解析或打印应用数据库凭据。
  [[ $(timeout 15 runuser -u postgres -- psql -X -qAt -v ON_ERROR_STOP=1 -d new_api -c 'SELECT 1') == 1 ]]
}

preflight() {
  [[ $EUID == 0 ]] || fail '生产命令必须由 root 受限入口执行'
  local tool
  for tool in python3 flock timeout runuser systemctl pg_dump pg_restore psql curl install mktemp readlink tar ln mv cp chmod rm mkdir sleep env; do
    command -v "$tool" >/dev/null || fail "缺少部署工具：$tool"
  done
  # 只检查文件存在，不读取配置；现有发布可能保留旧属主，不能在 check 中修正。
  [[ -d $RELEASES && -d $STATE && -f $ENVIRONMENT && -L $CURRENT ]] || fail '部署目录、配置或 current 链接缺失'
  previous=$(readlink -f -- "$CURRENT") # 解析真实目录，拒绝通过链接跳出 releases
  [[ $previous == "$RELEASES/"* && ${previous#"$RELEASES/"} != */* && -x $previous/new-api ]] || fail 'current 必须指向 releases 下的单层发布目录'
  # is-active 一次接收多个服务时只要求任意一个活跃，必须逐个检查。
  for tool in new-api caddy postgresql redis-server; do
    systemctl is-active --quiet "$tool" || fail "现有服务未运行：$tool"
  done
  database_ready || fail 'PostgreSQL 连通性检查失败'
  local version
  version=$(binary_version "$previous") # 使用当前程序报告的版本，不假定旧发布也有清单
  [[ -n $version && $version != *$'\n'* ]] || fail '现有程序版本无效'
  health "$version" http://127.0.0.1:3000 || fail '现有本机版本健康检查失败'
  health "$version" "$PUBLIC_URL" || fail '现有公网版本健康检查失败'
}

switch_current() {
  # 同一文件系统内替换链接，不原地覆盖运行中的二进制。
  # 失败切换可能已留下暂存链接，恢复时允许安全替换自己的链接。
  ln -sfn -- "$1" "$work/current" || return 1
  mv -Tf -- "$work/current" "$CURRENT"
}

finish() {
  local result=$? # 先保存原始退出码，清理动作不能把失败变成功
  trap - EXIT # 避免错误恢复触发第二次退出处理
  set +e # 清理或恢复失败必须报告，不能截断剩余的错误处理。
  if ((result != 0 && start_attempted)); then
    systemctl stop new-api || printf '警告：无法停止失败的新版本，请立即检查服务\n' >&2
    printf '新程序可能已迁移数据，禁止自动回滚二进制；服务已请求停止。备份：%s\n' "$backup" >&2
    printf '当前链接保留用于排障；恢复数据库和旧程序需人工确认。旧版本：%s\n' "$previous" >&2
  elif ((result != 0 && stopped)); then
    # 新程序尚未启动，数据库未由新版本打开；允许恢复原链接与原服务。
    if ! switch_current "$previous" || ! systemctl start new-api; then
      printf '旧服务自动恢复失败，需人工处理；备份：%s\n' "$backup" >&2
    else
      printf '新程序未启动，已恢复原链接和旧服务；备份：%s\n' "$backup" >&2
    fi
  fi
  [[ -z $work ]] || rm -rf -- "$work" # 仅删除自己的 root 暂存目录。
  exit "$result" # 保留最初失败状态，即便旧服务已成功恢复。
}

receive_archive() {
  # stdin 直接接收 SSH 流，不允许客户端指定服务器文件路径。
  # 多读一个字节用于检测超限，避免先完整接收再发现磁盘被占满。
  timeout 300 python3 -c '
import sys
limit = int(sys.argv[2])
with open(sys.argv[1], "xb") as output:
    total = 0
    while True:
        chunk = sys.stdin.buffer.read(min(1024 * 1024, limit + 1 - total))
        if not chunk:
            break
        total += len(chunk)
        if total > limit:
            sys.exit("输入压缩包超过 512 MiB")
        output.write(chunk)
' "$work/release.tar.gz" "$MAX_BYTES"
}

deploy() {
  local release=$1 revision=$2 digest=$3 target="$RELEASES/$1"
  [[ $EUID == 0 ]] || fail '生产命令必须由 root 受限入口执行'
  # 远端锁覆盖备份、切换和健康检查，即使两个工作流绕开 GitHub 并发组也不能交叠。
  exec 9>/run/new-api-ci-deploy.lock
  flock -n 9 || fail '已有部署进行中'
  preflight # 持锁后再次确认环境与旧程序状态，避免并发读取过时目标
  [[ ! -e $target && ! -L $target ]] || fail '发布目录已存在，禁止覆盖'
  work=$(mktemp -d /opt/new-api/.ci-deploy.XXXXXXXX)
  trap finish EXIT
  trap 'exit 130' HUP INT TERM # SSH 中断同样经过数据库迁移阶段边界处理。
  receive_archive
  validate_package "$work/release.tar.gz" "$release" "$revision" "$digest" # 保持旧服务运行，先校验完整包
  # 新目录由 root 创建；不继承本机打包时的 UID、GID 或写权限。
  install -d -m 755 -o root -g root "$target" # 发布目录应用用户只读，所有文件由 root 写入
  validate_package "$work/release.tar.gz" "$release" "$revision" "$digest" "$target"
  [[ $(binary_version "$target") == "$release" ]] || fail '二进制链接版本与清单不匹配'
  stopped=1 # 即使 stop 失败，也按需要恢复旧服务处理。
  systemctl stop new-api # 停止写入后再备份，使数据库与运行目录处于同一停服窗口
  install -d -m 700 -o root -g root "$BACKUPS"
  backup="$BACKUPS/$release" # 使用发布标识关联数据库和文件恢复点
  mkdir -m 700 -- "$backup" # 不覆盖同名备份，保留每次发布的恢复点。
  runuser -u postgres -- pg_dump -Fc new_api > "$backup/database.dump" # 命令失败立即中止，绝不启动未备份的新版本
  pg_restore --list "$backup/database.dump" > /dev/null # 只检查结构，不宣称完成恢复演练。
  cp -- "$ENVIRONMENT" "$backup/new-api.env" # 保留持久密钥，发布过程不能重新生成或覆盖线上配置
  tar -czf "$backup/state.tar.gz" -C "$STATE" . # 只归档运行目录，不把根目录或日志混入恢复数据
  chmod 600 "$backup/database.dump" "$backup/new-api.env" "$backup/state.tar.gz"
  switch_current "$target" # 备份全部完成后才改变发布指向
  start_attempted=1 # 以下命令失败也可能已有部分迁移，不能切回旧程序。
  systemctl start new-api # 从这里起数据库兼容性无法通过重启旧程序推定
  # readiness 有明确上限，服务启动成功不等于应用已完成初始化。
  local attempt ready=0 # 未观察到明确健康结果时始终保持未就绪状态
  for attempt in {1..30}; do
    if systemctl is-active --quiet new-api && health "$release" http://127.0.0.1:3000; then
      ready=1
      break
    fi
    sleep 2
  done
  ((ready)) || fail '新版本本机健康检查失败' # 本机未完成初始化时不把公网旧响应当作成功
  health "$release" "$PUBLIC_URL" || fail '新版本公网健康检查失败'
  database_ready || fail '新版本数据库连通性检查失败'
  printf '发布成功：%s；备份：%s\n' "$release" "$backup"
}

# 原始 SSH 字符串只按参数分割，不执行其中的 shell、重定向或变量替换。
if [[ ${SSH_ORIGINAL_COMMAND+x} ]]; then
  [[ $# == 0 && $SSH_ORIGINAL_COMMAND =~ [^[:space:]] && $SSH_ORIGINAL_COMMAND != *$'\n'* && $SSH_ORIGINAL_COMMAND != *$'\r'* ]] || { printf '拒绝非法 SSH 命令\n' >&2; exit 2; }
  read -r -a arguments <<< "$SSH_ORIGINAL_COMMAND" || { printf '拒绝空白 SSH 命令\n' >&2; exit 2; }
  set -- "${arguments[@]}"
  [[ ${1:-} == check || ${1:-} == deploy ]] || { printf '拒绝未授权 SSH 命令\n' >&2; exit 2; }
elif [[ ${1:-} == --validate-package ]]; then
  [[ $# == 5 ]] && validate_identity "$3" "$4" "$5" || { printf '拒绝非法本地校验命令参数\n' >&2; exit 2; }
  validate_package "$2" "$3" "$4" "$5" # 本地分支在任何生产路径访问之前结束。
  printf '发布包校验通过\n'
  exit 0
fi

# 先验证参数，再触碰生产状态；单独 check 不创建锁、目录或临时文件。
if [[ $# == 1 && $1 == check ]]; then
  cd /
  preflight
  printf '部署环境检查通过\n'
elif [[ $# == 4 && $1 == deploy ]] && validate_identity "$2" "$3" "$4"; then
  cd /
  deploy "$2" "$3" "$4"
else
  printf '拒绝非法命令；仅允许 check 或 deploy <release> <revision> <sha256>\n' >&2
  exit 2
fi
