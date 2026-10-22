#!/usr/bin/env python3
"""Exercise publication transitions offline; the fake gh never uses the network."""

import base64
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
ARCHIVE = "myarch-buildkit-linux-x86_64.tar.gz"
COMMIT = "a" * 40
FAKE_GH = r'''#!/usr/bin/env python3
import base64, json, os, pathlib, sys
path = pathlib.Path(os.environ["PUBLISH_TEST_STATE"])
state = json.loads(path.read_text())
args = sys.argv[1:]
state["calls"].append(args)
def save():
    path.write_text(json.dumps(state))
def fail(message):
    save()
    print(message, file=sys.stderr)
    sys.exit(1)
def option(name):
    return args[args.index(name) + 1]
if args[0] == "api":
    endpoint = next(arg for arg in args if arg.startswith("repos/"))
    if "/git/ref/tags/" in endpoint:
        if state.get("invalid_tag"):
            print("tree\t" + "a" * 40)
            save()
            sys.exit(0)
        kind = "tag" if state.get("annotated") else "commit"
        print(kind + "\t" + state.get("tag_commit", "a" * 40))
    elif "/git/tags/" in endpoint:
        print("commit\t" + state.get("tag_commit", "a" * 40))
    elif "/releases/tags/" in endpoint:
        if state.get("api_failure"):
            status = state.get("api_status", 503)
            if status:
                print("HTTP/2.0 " + str(status) + " Error\n")
            fail("server unavailable")
        if state["release"] is None:
            print("HTTP/2.0 404 Not Found\n")
            fail("not found")
        print("HTTP/2.0 200 OK\n")
    else:
        fail("unexpected API command")
elif args[:2] == ["release", "view"]:
    if state.get("view_failure"):
        fail("release metadata unavailable")
    if option("--json") == "isDraft":
        print(str(state["release"]["draft"]).lower())
    else:
        if state.get("asset_list_failure"):
            fail("asset list unavailable")
        print("\n".join(state["release"]["assets"]))
elif args[:2] == ["release", "download"]:
    if state.get("download_failure"):
        fail("asset download failed")
    asset = option("--pattern")
    content = state["release"]["assets"][asset]
    pathlib.Path(option("--dir"), asset).write_bytes(base64.b64decode(content))
elif args[:2] in (["release", "create"], ["release", "upload"]):
    if args[1] == "create":
        if state["release"] is not None:
            fail("release already exists")
        assert "--draft" in args and "--verify-tag" in args
        state["release"] = {"draft": True, "assets": {}}
    assert "--clobber" not in args
    for arg in args[3:]:
        file = pathlib.Path(arg)
        if file.is_file():
            assert file.name not in state["release"]["assets"]
            content = file.read_bytes()
            if state.get("corrupt_upload"):
                content += b"changed"
            state["release"]["assets"][file.name] = base64.b64encode(content).decode()
elif args[:2] == ["release", "edit"]:
    assert "--draft=false" in args
    state["release"]["draft"] = False
else:
    fail("unexpected gh command: " + repr(args))
save()
'''


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="myarch-publish-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.dist = self.root / "dist"
        self.dist.mkdir()
        payload = b"the tested release archive\n"
        checksum = hashlib.sha256(payload).hexdigest() + "  " + ARCHIVE + "\n"
        self.local_assets = {ARCHIVE: payload, "SHA256SUMS": checksum.encode()}
        for name, content in self.local_assets.items():
            (self.dist / name).write_bytes(content)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        (self.bin / "gh").write_text(FAKE_GH)
        (self.bin / "gh").chmod(0o755)
        self.state_path = self.root / "state.json"
        self.state = {"calls": [], "release": None}

    def existing(self, draft, assets=None):
        content = self.local_assets if assets is None else assets
        self.state["release"] = {
            "draft": draft,
            "assets": {name: base64.b64encode(data).decode() for name, data in content.items()},
        }

    def run_publish(self, success=True):
        self.state_path.write_text(json.dumps(self.state))
        env = os.environ.copy()
        env.update({
            "PATH": str(self.bin) + os.pathsep + env["PATH"],
            "PUBLISH_TEST_STATE": str(self.state_path),
            "DIST_DIR": str(self.dist),
            "GH_REPO": "example/myarch-buildkit",
            "RELEASE_TAG": "v0.3.0",
            "RELEASE_COMMIT": COMMIT,
        })
        result = subprocess.run(["bash", str(ROOT / "scripts/publish-release.sh")],
                                cwd=self.root, env=env, text=True, capture_output=True)
        self.state = json.loads(self.state_path.read_text())
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        return result

    def mutations(self):
        return [call[1] for call in self.state["calls"]
                if call[:2] in (["release", "create"], ["release", "upload"], ["release", "edit"])]

    def test_new_release_verified_before_publication(self):
        self.run_publish()
        self.assertEqual(self.mutations(), ["create", "edit"])
        self.assertFalse(self.state["release"]["draft"])
        calls = self.state["calls"]
        self.assertEqual([call[1] for call in calls[-3:]], ["download", "download", "edit"])

    def test_annotated_tag_is_resolved(self):
        self.state["annotated"] = True
        self.run_publish()
        self.assertTrue(any("/git/tags/" in " ".join(call) for call in self.state["calls"]))

    def test_matching_draft_published_without_reupload(self):
        self.existing(True)
        self.run_publish()
        self.assertEqual(self.mutations(), ["edit"])

    def test_partial_draft_uploads_only_missing_asset(self):
        self.existing(True, {ARCHIVE: self.local_assets[ARCHIVE], "notes.txt": b"keep me"})
        self.run_publish()
        self.assertEqual(self.mutations(), ["upload", "edit"])
        upload = next(call for call in self.state["calls"] if call[:2] == ["release", "upload"])
        self.assertIn(str(self.dist / "SHA256SUMS"), upload)
        self.assertNotIn(str(self.dist / ARCHIVE), upload)
        self.assertIn("notes.txt", self.state["release"]["assets"])

    def test_conflicting_draft_is_unchanged(self):
        self.existing(True, {ARCHIVE: b"different"})
        self.run_publish(False)
        self.assertEqual(self.mutations(), [])

    def test_matching_published_release_is_noop(self):
        self.existing(False)
        self.run_publish()
        self.assertEqual(self.mutations(), [])

    def test_conflicting_published_release_is_unchanged(self):
        self.existing(False, {**self.local_assets, ARCHIVE: b"different"})
        self.run_publish(False)
        self.assertEqual(self.mutations(), [])

    def test_incomplete_published_release_is_unchanged(self):
        self.existing(False, {ARCHIVE: self.local_assets[ARCHIVE]})
        self.run_publish(False)
        self.assertEqual(self.mutations(), [])

    def test_wrong_tag_commit_prevents_publication(self):
        self.state["tag_commit"] = "b" * 40
        self.run_publish(False)
        self.assertEqual(self.mutations(), [])

    def test_api_failure_does_not_create_release(self):
        for status in (503, 403, None):
            with self.subTest(status=status):
                self.state["api_failure"] = True
                self.state["api_status"] = status
                self.run_publish(False)
                self.assertEqual(self.mutations(), [])

    def test_tag_must_point_to_commit_or_annotated_tag(self):
        self.state["invalid_tag"] = True
        self.run_publish(False)
        self.assertEqual(self.mutations(), [])

    def test_metadata_failure_does_not_mutate_release(self):
        self.existing(True)
        self.state["view_failure"] = True
        self.run_publish(False)
        self.assertEqual(self.mutations(), [])

    def test_asset_list_failure_does_not_trigger_upload(self):
        self.existing(True)
        self.state["asset_list_failure"] = True
        self.run_publish(False)
        self.assertEqual(self.mutations(), [])

    def test_asset_download_failure_prevents_publication(self):
        self.existing(True)
        self.state["download_failure"] = True
        self.run_publish(False)
        self.assertEqual(self.mutations(), [])

    def test_changed_upload_remains_draft(self):
        self.state["corrupt_upload"] = True
        self.run_publish(False)
        self.assertEqual(self.mutations(), ["create"])
        self.assertTrue(self.state["release"]["draft"])

    def test_invalid_local_checksum_prevents_network(self):
        (self.dist / "SHA256SUMS").write_text("incorrect\n")
        self.run_publish(False)
        self.assertEqual(self.state["calls"], [])


if __name__ == "__main__":
    unittest.main()
