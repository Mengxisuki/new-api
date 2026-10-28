"""验证受限 SSH 命令和发布归档，不连接服务器或执行生产命令。"""

import hashlib
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "ci-deploy.sh"
REVISION = "123456789abc" + "d" * 28  # 提交前缀必须与发布目录一致。
RELEASE = "oak-123456789abc-12345-1"  # 模拟 GitHub run_id 与 attempt。
MAX_BYTES = 512 * 1024 * 1024  # 只构造超限头，不实际写入巨量数据。


class DeploymentContractTest(unittest.TestCase):
    """测试真实命令入口，以返回码和文件副作用判定行为。"""

    def setUp(self):
        self.workspace = tempfile.TemporaryDirectory()  # 每个案例独立清理。
        self.addCleanup(self.workspace.cleanup)
        self.directory = Path(self.workspace.name) # 文件副作用只允许出现在各案例专用临时目录
        # ELF 头只用于文件格式验证，本地校验不能执行此二进制。
        elf = bytearray(64)
        elf[:6] = b"\x7fELF\x02\x01"  # 64 位小端格式。
        elf[18:20] = b"\x3e\x00"  # Linux amd64 的机器类型。
        self.files = {
            "new-api": bytes(elf),
            "LICENSE": b"project license\n",  # 固定许可文件不可遗漏。
            "NOTICE": b"project notice\n",
            "THIRD-PARTY-LICENSES.md": b"third party licenses\n",
            "VERSION": (RELEASE + "\n").encode(),  # 与命令参数逐字一致。
            "REVISION": (REVISION + "\n").encode(),
        }

    def package(self, extra=None, omit=None):
        """构造确定性最小归档；额外成员用于攻击边界测试。"""
        archive = self.directory / "release.tar.gz" # 反复构造输入也不会写项目或生产目录
        with tarfile.open(archive, "w:gz") as output:
            for name, data in self.files.items():
                if name == omit:  # 缺失文件必须由校验器实际识别。
                    continue
                entry = tarfile.TarInfo(name)
                entry.size = len(data)
                output.addfile(entry, io.BytesIO(data))
            if extra is not None:
                output.addfile(*extra)  # 保留恶意头的原始类型和路径。
        digest = hashlib.sha256(archive.read_bytes()).hexdigest() # 测试提供真实包摘要，不 mock 验签过程
        return archive, digest

    def validate(self, archive, digest, release=RELEASE, revision=REVISION):
        # 清除外层 SSH 状态，保证测试走公开本地校验入口。
        environment = os.environ.copy() # 保留真实 Python 和 shell 环境，仅隔离 SSH 入口选择
        environment.pop("SSH_ORIGINAL_COMMAND", None)
        return subprocess.run(
            ["bash", str(SCRIPT), "--validate-package", str(archive), release, revision, digest],
            env=environment, capture_output=True, text=True, timeout=10,
        )

    def test_valid_fixed_package_is_accepted_without_extraction(self):
        archive, digest = self.package()
        before = set(self.directory.iterdir())  # 校验不能解压或启动应用。
        result = self.validate(archive, digest)
        self.assertEqual(result.returncode, 0, result.stderr) # 成功包必须实际被接受，防止所有输入一律失败也通过测试
        self.assertEqual(set(self.directory.iterdir()), before) # 本地校验不能生成解压目录或额外配置
        self.assertEqual(hashlib.sha256(archive.read_bytes()).hexdigest(), digest) # 验证入口不能修改发布包本身

    def test_wrong_checksum_is_rejected(self):
        archive, _ = self.package() # 保持包内容有效，单独破坏调用方提供的摘要
        self.assertNotEqual(self.validate(archive, "0" * 64).returncode, 0) # 返回失败状态供工作流阻止后续发布

    def test_oversized_compressed_file_is_rejected_before_hashing(self):
        archive = self.directory / "oversized.tar.gz"
        # 使用稀疏文件只验证长度边界，不写入巨量数据或执行压力测试。
        with archive.open("wb") as output:
            output.truncate(MAX_BYTES + 1)
        self.assertNotEqual(self.validate(archive, "0" * 64).returncode, 0)

    def test_missing_required_license_is_rejected(self):
        archive, digest = self.package(omit="NOTICE") # 固定文件缺失是发布失败，不能用默认内容补齐
        self.assertNotEqual(self.validate(archive, digest).returncode, 0)

    def test_metadata_must_match_command(self):
        # 错误清单不能因为包校验和正确就被当作目标版本发布。
        for name in ("VERSION", "REVISION"): # 分别验证版本和完整提交，不把二者合成一个模糊身份
            with self.subTest(name=name):
                original = self.files[name] # 下一个子案例恢复初始输入，不依赖运行顺序
                self.files[name] = b"different\n" # 内容错误但校验和真实，必须由元数据校验拒绝
                archive, digest = self.package()
                self.assertNotEqual(self.validate(archive, digest).returncode, 0) # 任何成员攻击都必须使整个包失败
                self.files[name] = original

    def test_release_prefix_must_match_revision(self):
        archive, digest = self.package()
        # 目录、完整提交与包内清单必须共同对应同一个发布。
        self.assertNotEqual(self.validate(archive, digest, revision="a" * 40).returncode, 0)

    def test_archive_rejects_traversal_links_duplicates_and_extra_files(self):
        # 所有攻击都经过真实 tar 解析，不 mock 文件名判断。
        for name, kind in (
            ("../outside", tarfile.REGTYPE),
            ("/tmp/outside", tarfile.REGTYPE),
            ("extra", tarfile.REGTYPE),
            ("LICENSE", tarfile.REGTYPE),  # 重名成员不能覆盖先前许可文件。
            ("new-api", tarfile.SYMTYPE),  # 合法名称也不能承载符号链接。
            ("new-api", tarfile.LNKTYPE),  # 同样禁止归档内部硬链接。
            ("directory/", tarfile.DIRTYPE),
        ):
            with self.subTest(name=name, kind=kind):
                entry = tarfile.TarInfo(name)
                entry.type = kind # 直接构造 tar 头以覆盖文件系统之外的链接类型
                entry.linkname = "new-api" if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE) else ""
                omitted = name if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE) else None
                archive, digest = self.package(extra=(entry, None), omit=omitted)
                self.assertNotEqual(self.validate(archive, digest).returncode, 0)
        self.assertFalse((self.directory.parent / "outside").exists())

    def test_oversized_member_is_rejected_before_reading_payload(self):
        # 只有超限声明，不创建 512 MiB 文件；校验应先拒绝头部。
        entry = tarfile.TarInfo("new-api")
        entry.size = MAX_BYTES + 1
        archive, digest = self.package(omit="new-api", extra=(entry, None))
        self.assertNotEqual(self.validate(archive, digest).returncode, 0)

    def test_non_elf_binary_is_rejected(self):
        self.files["new-api"] = b"#!/bin/sh\nexit 0\n"  # 发布目标固定 Linux amd64。
        archive, digest = self.package()
        self.assertNotEqual(self.validate(archive, digest).returncode, 0)

    def test_pax_and_long_name_headers_are_rejected_before_payload(self):
        # tarfile 自动迭代会隐藏这类头；root 处理器必须先拒绝特殊类型。
        for kind in (tarfile.XHDTYPE, tarfile.GNUTYPE_LONGNAME):
            with self.subTest(kind=kind):
                entry = tarfile.TarInfo("new-api")  # 使用白名单名称隔离类型判断。
                entry.type = kind
                entry.size = MAX_BYTES + 1  # 不提供载荷，必须在尝试读取前拒绝。
                archive, digest = self.package(omit="new-api", extra=(entry, None))
                result = self.validate(archive, digest)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("只允许普通文件", result.stderr)

    def test_ssh_rejects_shell_syntax_and_local_validation_option(self):
        sentinel = self.directory / "executed"
        # 入口不能将原始字符串交给 eval、bash -c 或系统 shell。
        commands = (
            "", "   ", "check extra", "check\ncheck", "check; true",
            f"check $(touch {sentinel})", "--validate-package anything",
            f"deploy {RELEASE} {REVISION} {'0' * 64}; touch {sentinel}",
        )
        for command in commands:
            with self.subTest(command=command):
                environment = os.environ.copy()
                environment["SSH_ORIGINAL_COMMAND"] = command # 模拟 sshd 传给受限入口的原始命令字符串
                result = subprocess.run(
                    ["bash", str(SCRIPT)], env=environment,
                    capture_output=True, text=True, timeout=10,
                )
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertIn("命令", result.stderr)  # 必须在生产预检前拒绝。
        self.assertFalse(sentinel.exists())  # Shell 替换从未执行。


if __name__ == "__main__":
    unittest.main()  # 直接运行与 CI 的 unittest discovery 使用同一测试集。
