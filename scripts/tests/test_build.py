#!/usr/bin/env python3
"""Exercise release packaging offline in disposable repositories.

The fixture is a tiny real Go CLI. These tests never run the actual setup tool,
contact a package registry, or publish a release. Set GO to the pinned compiler
when it is not available on PATH.
"""

import hashlib
import io
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
ARCHIVE = "myarch-buildkit-linux-x86_64.tar.gz"
EPOCH = "1700000000"
PAYLOAD = {
    "myarch-buildkit",
    "BUILD-INFO",
    "SHA256SUMS",
    "LICENSE",
    "README.md",
    "CONTRIBUTING.md",
    "docs/user-guide.md",
}
MAIN = r'''package main

import (
    "crypto/sha256"
    "encoding/json"
    "fmt"
    "os"
)

func main() {
    if len(os.Args) < 2 { os.Exit(90) }
    switch os.Args[1] {
    case "--version":
        fmt.Println("myarch-buildkit", BuildID)
        path, err := os.Executable()
        if err != nil { panic(err) }
        data, err := os.ReadFile(path)
        if err != nil { panic(err) }
        fmt.Printf("Executable SHA256: %x\n", sha256.Sum256(data))
    case "--help", "help":
        fmt.Println("Usage: myarch-buildkit plan|check|--settings-template")
    case "plan":
        fmt.Println("myarch-buildkit", BuildID, "Plan: no changes")
    case "--settings-template", "settings-template":
        fmt.Println(`{"schema_version":2,"package_groups":["core"]}`)
    case "check":
        json.NewEncoder(os.Stdout).Encode(map[string]any{
            "schema_version": 1, "build_id": BuildID,
            "status": "incomplete", "check_error": "fixture platform",
        })
        os.Exit(2)
    default:
        // Any accidental invocation of setup/apply is a test failure.
        os.Exit(91)
    }
}
'''


def digest(data):
    return hashlib.sha256(data).hexdigest()


def run(args, *, cwd, env, umask=0o022, timeout=180):
    return subprocess.run(
        [str(arg) for arg in args], cwd=cwd, env=env, umask=umask,
        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
        timeout=timeout, check=False,
    )


class BuildReleaseTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        for command in ("git", "bash", "tar", "sha256sum"):
            if not shutil.which(command):
                raise RuntimeError(f"required command is unavailable: {command}")
        cls.temp = tempfile.TemporaryDirectory(prefix="myarch-build-tests-")
        cls.addClassCleanup(cls.temp.cleanup)
        cls.base = Path(cls.temp.name)
        cls.fixture = cls.base / "fixture repository with spaces"
        cls.fixture.mkdir()
        (cls.fixture / "scripts").mkdir()
        (cls.fixture / "cmd/myarch-buildkit").mkdir(parents=True)
        (cls.fixture / "docs").mkdir()
        for name in ("build.sh", "verify-package.sh", "validate-release.sh"):
            shutil.copy2(ROOT / "scripts" / name, cls.fixture / "scripts" / name)
        for name in (".go-version", "cmd/myarch-buildkit/version.go"):
            shutil.copy2(ROOT / name, cls.fixture / name)
        (cls.fixture / "go.mod").write_text("module myarch-buildkit\n\ngo 1.23\n")
        (cls.fixture / "cmd/myarch-buildkit/main.go").write_text(MAIN)
        (cls.fixture / ".gitignore").write_text("/bin/\n/dist/\n")
        for name in ("LICENSE", "README.md", "CONTRIBUTING.md", "docs/user-guide.md"):
            (cls.fixture / name).write_text(f"Release regression fixture: {name}\n")
        cls.version = (cls.fixture / "cmd/myarch-buildkit/version.go").read_text().split('var BuildID = "')[1].split('"')[0]
        cls.env = os.environ.copy()
        # Keep all test build inputs independent of the invoking shell's options.
        # In particular, never let GIT_DIR/GIT_WORK_TREE redirect fixture writes.
        for key in list(cls.env):
            if key.startswith("GIT_"):
                cls.env.pop(key)
        for key in ("VERSION", "SOURCE_DATE_EPOCH", "GOOS", "GOARCH", "GOAMD64", "CGO_ENABLED", "GOFLAGS"):
            cls.env.pop(key, None)
        cls.env.update({
            "GOENV": "off", "GOWORK": "off", "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off",
            "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_AUTHOR_DATE": f"{EPOCH} +0000", "GIT_COMMITTER_DATE": f"{EPOCH} +0000",
        })
        cls.env["GO"] = shutil.which(cls.env.get("GO", "go")) or cls.env.get("GO", "go")
        compiler = run([cls.env["GO"], "env", "GOVERSION"], cwd=cls.fixture, env=cls.env)
        if compiler.returncode:
            raise RuntimeError(f"cannot query the selected Go compiler:\n{compiler.stdout}")
        cls.go_version = compiler.stdout.strip().removeprefix("go")
        # Exercise pin enforcement on any CI compiler without downloading one.
        (cls.fixture / ".go-version").write_text(cls.go_version + "\n")
        cls.env["VERSION"] = cls.version
        for args in (
            ["git", "init", "--quiet"],
            ["git", "add", "."],
            ["git", "-c", "user.name=Release Test", "-c", "user.email=release-test@example.invalid", "commit", "--quiet", "-m", "Fixture"],
            ["bash", cls.fixture / "scripts/build.sh", "package"],
        ):
            result = run(args, cwd=cls.fixture, env=cls.env)
            if result.returncode:
                raise RuntimeError(f"fixture command failed: {args}\n{result.stdout}")
        cls.baseline = (cls.fixture / "dist" / ARCHIVE).read_bytes()

    def setUp(self):
        self.work = self.base / self._testMethodName
        shutil.copytree(self.fixture, self.work)
        self.outside = self.base / (self._testMethodName + " outside cwd")
        self.outside.mkdir()

    def command(self, script, *args, overrides=None, umask=0o022):
        env = self.env.copy()
        env.update(overrides or {})
        return run(["bash", self.work / "scripts" / script, *args], cwd=self.outside, env=env, umask=umask)

    def assert_ok(self, result):
        self.assertEqual(result.returncode, 0, result.stdout)

    def snapshot_outputs(self):
        return {
            str(path.relative_to(self.work)): path.read_bytes()
            for folder in ("bin", "dist")
            for path in (self.work / folder).rglob("*")
            if path.is_file()
        }

    def archive_members(self):
        with tarfile.open(self.work / "dist" / ARCHIVE, "r:gz") as archive:
            return [(item, archive.extractfile(item).read() if item.isfile() else b"") for item in archive.getmembers()]

    def archive_metadata(self):
        contents = next(data for item, data in self.archive_members() if item.name == "BUILD-INFO")
        return dict(line.split("=", 1) for line in contents.decode().splitlines())

    def rewrite_archive(self, transform, *, refresh_inner=False):
        members = transform(self.archive_members())
        if refresh_inner:
            checksums = "".join(
                f"{digest(data)}  {item.name}\n"
                for item, data in members if item.isfile() and item.name != "SHA256SUMS"
            ).encode()
            members = [(item, checksums if item.name == "SHA256SUMS" else data) for item, data in members]
        archive_path = self.work / "dist" / ARCHIVE
        with tarfile.open(archive_path, "w:gz") as archive:
            for item, data in members:
                if item.isfile():
                    item.size = len(data)
                    archive.addfile(item, io.BytesIO(data))
                else:
                    archive.addfile(item)
        # Keep the outer checksum valid to reach the archive's internal checks.
        (self.work / "dist/SHA256SUMS").write_text(f"{digest(archive_path.read_bytes())}  {ARCHIVE}\n")

    def assert_rejected(self):
        result = self.command("verify-package.sh", self.work / "dist")
        self.assertNotEqual(result.returncode, 0, result.stdout)

    def test_package_verifies_from_an_unrelated_working_directory(self):
        self.assert_ok(self.command("verify-package.sh"))
        self.assert_ok(self.command("verify-package.sh", self.work / "dist"))
        self.assertEqual(list(self.outside.iterdir()), [])

    def test_build_and_release_validation_work_outside_repository(self):
        self.assert_ok(self.command("validate-release.sh", self.version))
        self.assert_ok(self.command("build.sh", "build"))
        result = run([self.work / "bin/myarch-buildkit", "--version"], cwd=self.outside, env=self.env)
        self.assert_ok(result)
        self.assertIn(f"myarch-buildkit {self.version}", result.stdout)
        self.assertEqual(list(self.outside.iterdir()), [])

    def test_archive_has_complete_metadata_and_normalized_members(self):
        members = self.archive_members()
        self.assertEqual({item.name for item, _ in members}, PAYLOAD)
        self.assertEqual(len(members), len(PAYLOAD), "duplicate archive member")
        for item, _ in members:
            self.assertTrue(item.isfile(), item.name)
            self.assertEqual((item.uid, item.gid), (0, 0), item.name)
            self.assertEqual(item.mtime, int(EPOCH), item.name)
            self.assertEqual(item.mode, 0o755 if item.name == "myarch-buildkit" else 0o644, item.name)
        metadata = self.archive_metadata()
        self.assertEqual(metadata["version"], self.version)
        self.assertEqual(metadata["go_version"].removeprefix("go"), self.go_version.removeprefix("go"))
        self.assertEqual(metadata["goos"], "linux")
        self.assertEqual(metadata["goarch"], "amd64")
        self.assertEqual(metadata["goamd64"], "v1")
        self.assertEqual(metadata["cgo_enabled"], "0")
        self.assertEqual(metadata["source_date_epoch"], EPOCH)
        revision = run(["git", "rev-parse", "HEAD"], cwd=self.work, env=self.env)
        self.assert_ok(revision)
        self.assertEqual(metadata["revision"], revision.stdout.strip())
        self.assertIn(metadata["dirty"], ("false", "0"))

    def test_package_is_identical_after_source_mtime_and_umask_change(self):
        for path in self.work.rglob("*"):
            if path.is_file() and ".git" not in path.parts:
                os.utime(path, (int(EPOCH) + 10000, int(EPOCH) + 10000))
        self.assert_ok(self.command("build.sh", "package", umask=0o077))
        self.assertEqual((self.work / "dist" / ARCHIVE).read_bytes(), self.baseline)
        self.assert_ok(self.command("build.sh", "package", umask=0o002))
        self.assertEqual((self.work / "dist" / ARCHIVE).read_bytes(), self.baseline)

    def test_package_ignores_hostile_target_environment(self):
        self.assert_ok(self.command("build.sh", "package", overrides={
            "GOOS": "windows", "GOARCH": "arm64", "GOAMD64": "v4", "CGO_ENABLED": "1",
        }))
        self.assertEqual((self.work / "dist" / ARCHIVE).read_bytes(), self.baseline)
        self.assert_ok(self.command("verify-package.sh"))

    def test_explicit_source_date_epoch_controls_archive_metadata(self):
        epoch = str(int(EPOCH) + 3600)
        self.assert_ok(self.command("build.sh", "package", overrides={"SOURCE_DATE_EPOCH": epoch}))
        self.assertEqual(self.archive_metadata()["source_date_epoch"], epoch)
        self.assertTrue(all(item.mtime == int(epoch) for item, _ in self.archive_members()))
        self.assert_ok(self.command("verify-package.sh"))

    def test_development_package_records_dirty_source_revision(self):
        (self.work / "README.md").write_text("Locally modified documentation\n")
        self.assert_ok(self.command("build.sh", "package", overrides={"VERSION": ""}))
        metadata = self.archive_metadata()
        self.assertEqual(metadata["dirty"], "true")
        self.assertEqual(metadata["version"], f"{self.version}-dev.{metadata['revision'][:12]}.dirty")
        self.assert_ok(self.command("verify-package.sh"))

    def test_invalid_inputs_do_not_replace_published_outputs(self):
        before = self.snapshot_outputs()
        for overrides in (
            {"VERSION": "../invalid"},
            {"VERSION": "v999.0.0"},
            {"SOURCE_DATE_EPOCH": "yesterday"},
            {"SOURCE_DATE_EPOCH": "-1"},
            {"SOURCE_DATE_EPOCH": "9999999999999999999999999999"},
            {"GO": str(self.work / "compiler-does-not-exist")},
        ):
            with self.subTest(overrides=overrides):
                result = self.command("build.sh", "package", overrides=overrides)
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertEqual(self.snapshot_outputs(), before)

    def test_failed_compilation_preserves_previous_package(self):
        before = self.snapshot_outputs()
        with (self.work / "cmd/myarch-buildkit/main.go").open("a") as source:
            source.write("\nthis is not valid Go\n")
        result = self.command("build.sh", "package")
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual(self.snapshot_outputs(), before)
        self.assertEqual(list((self.work / "dist").glob(".package.*")), [])

    def test_wrong_compiler_version_is_rejected_without_publication(self):
        compiler = self.base / "wrong-go-version"
        compiler.write_text('#!/usr/bin/env bash\nif [[ "$1" == version ]]; then echo "go version go0.0.0 linux/amd64"; else echo go0.0.0; fi\n')
        compiler.chmod(0o755)
        before = self.snapshot_outputs()
        result = self.command("build.sh", "package", overrides={"GO": str(compiler)})
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual(self.snapshot_outputs(), before)

    def test_outer_checksum_corruption_is_rejected(self):
        (self.work / "dist/SHA256SUMS").write_text(f"{'0' * 64}  {ARCHIVE}\n")
        self.assert_rejected()

    def test_inner_checksum_corruption_is_rejected(self):
        self.rewrite_archive(lambda members: [(item, b"corrupted documentation\n" if item.name == "README.md" else data) for item, data in members])
        self.assert_rejected()

    def test_non_elf_binary_is_rejected_even_with_valid_checksums(self):
        sentinel = self.outside / "must-not-execute"
        replacement = f"#!/usr/bin/env bash\ntouch '{sentinel}'\nexit 0\n".encode()
        self.rewrite_archive(lambda members: [(item, replacement if item.name == "myarch-buildkit" else data) for item, data in members], refresh_inner=True)
        self.assert_rejected()
        self.assertFalse(sentinel.exists(), "verification executed an unvalidated binary")

    def test_wrong_elf_architecture_is_rejected_with_valid_checksums(self):
        def change_architecture(members):
            for item, data in members:
                if item.name == "myarch-buildkit":
                    # ELF e_machine 183 is AArch64; bytes remain ELF64 little-endian.
                    data = data[:18] + bytes((183, 0)) + data[20:]
                yield item, data

        self.rewrite_archive(lambda members: list(change_architecture(members)), refresh_inner=True)
        self.assert_rejected()

    def test_binary_version_must_match_archive_metadata(self):
        self.rewrite_archive(lambda members: [
            (item, data.replace(f"version={self.version}\n".encode(), b"version=v999.0.0\n") if item.name == "BUILD-INFO" else data)
            for item, data in members
        ], refresh_inner=True)
        self.assert_rejected()

    def test_reported_binary_hash_must_match_archive_contents(self):
        source = MAIN.replace('sha256.Sum256(data)', 'sha256.Sum256(append(data, 0))')
        (self.work / "cmd/myarch-buildkit/main.go").write_text(source)
        self.assert_ok(self.command("build.sh", "package"))
        self.assert_rejected()

    def test_malformed_cli_json_is_rejected(self):
        variants = {
            "settings": MAIN.replace('{"schema_version":2,"package_groups":["core"]}', '{"schema_version":2'),
            "check": MAIN.replace('case "check":', 'case "check":\n        fmt.Println(`{"status":"incomplete"`)\n        os.Exit(2)'),
            "multiple_documents": MAIN.replace('case "check":', 'case "check":\n        fmt.Println(`{"status":"incomplete","check_error":"first"}`)'),
        }
        for name, source in variants.items():
            with self.subTest(name=name):
                (self.work / "cmd/myarch-buildkit/main.go").write_text(source)
                self.assert_ok(self.command("build.sh", "package"))
                self.assert_rejected()

    def test_unexpected_archive_member_is_rejected(self):
        extra = tarfile.TarInfo("unexpected.txt")
        self.rewrite_archive(lambda members: members + [(extra, b"unexpected\n")], refresh_inner=True)
        self.assert_rejected()

    def test_duplicate_archive_member_is_rejected(self):
        self.rewrite_archive(lambda members: members + [next((item, data) for item, data in members if item.name == "README.md")])
        self.assert_rejected()

    def test_traversal_archive_member_is_rejected_before_extraction(self):
        extra = tarfile.TarInfo("../escaped-file")
        self.rewrite_archive(lambda members: members + [(extra, b"escaped\n")])
        self.assert_rejected()
        self.assertEqual(list(self.base.rglob("escaped-file")), [])

    def test_symlink_archive_member_is_rejected(self):
        def symlink_readme(members):
            for item, data in members:
                if item.name == "README.md":
                    item.type = tarfile.SYMTYPE
                    item.linkname = "/etc/passwd"
                    data = b""
                yield item, data

        self.rewrite_archive(lambda members: list(symlink_readme(members)))
        self.assert_rejected()


if __name__ == "__main__":
    unittest.main(verbosity=2)
