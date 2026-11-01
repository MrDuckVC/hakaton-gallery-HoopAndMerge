"""Safety checks for kit generation and integrity; no Docker or real models."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace

spec = importlib.util.spec_from_file_location("offline_bundle", Path(__file__).with_name("offline-bundle.py"))
kit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(kit)


class OfflineKitTest(unittest.TestCase):
    def test_runtime_version_is_read_from_client_and_must_match_pin(self):
        for output in ["Warning: client version is 0.40.13", "Warning: client version is 0.39.0", "unknown runtime"]:
            with self.subTest(output=output), patch.object(kit.subprocess, "run", return_value=SimpleNamespace(stdout="", stderr=output, returncode=0)) as run:
                if "0.40.13" in output:
                    self.assertEqual(kit.check_runtime(Path("runtime")), "0.40.13")
                    self.assertEqual(run.call_args.kwargs["env"]["OLLAMA_HOST"], "127.0.0.1:1")
                else:
                    with self.assertRaisesRegex(ValueError, "Ollama"):
                        kit.check_runtime(Path("runtime"))

    def test_kit_template_preserves_absolute_photo_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            source_photos = Path(directory) / "existing photos"
            source_photos.mkdir()
            (source_photos / "private.jpg").write_bytes(b"keep")
            template = "FRONTEND_DIST_DIR=./data/frontend\nPHOTO_DIR=./data/photos\n"
            result = kit.kit_environment(template, source_photos)
            self.assertIn("PHOTO_DIR='" + str(source_photos) + "'", result)
            self.assertIn("FRONTEND_DIST_DIR=./frontend", result)
            self.assertEqual((source_photos / "private.jpg").read_bytes(), b"keep")

    def test_refuse_existing_output_without_touching_it(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            marker = root / "private"
            marker.write_text("keep")
            with self.assertRaisesRegex(ValueError, "output exists"):
                kit.prepare(root, root, root, root)
            self.assertEqual(marker.read_text(), "keep")

    def test_integrity_detects_modified_missing_and_extra_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            file = root / "gallery"
            file.write_bytes(b"executable")
            manifest = {"files": kit.inventory(root)}
            (root / "bundle-manifest.json").write_text(json.dumps(manifest))
            kit.verify(root)
            file.write_bytes(b"changed")
            with self.assertRaises(ValueError):
                kit.verify(root)
            file.unlink()
            with self.assertRaises(ValueError):
                kit.verify(root)
            file.write_bytes(b"executable")
            (root / "secret.env").write_text("unexpected")
            with self.assertRaises(ValueError):
                kit.verify(root)

    def test_escaping_runtime_symlink_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "link").symlink_to("/etc/passwd")
            with self.assertRaises(ValueError):
                kit.inventory(root)

    def test_current_ollama_manifests_include_only_referenced_blobs(self):
        with tempfile.TemporaryDirectory() as directory:
            store = Path(directory)
            data = b"local-model-layer"
            value = "sha256:" + hashlib.sha256(data).hexdigest()
            blob = Path("blobs") / value.replace(":", "-")
            (store / blob).parent.mkdir()
            (store / blob).write_bytes(data)
            expected = {blob}
            for tag in kit.TAGS:
                name, version = tag.split(":")
                manifest = Path("manifests-v2/ollama.com/library") / name / version
                (store / manifest).parent.mkdir(parents=True)
                (store / manifest).write_text(json.dumps({"config": {"digest": value}, "layers": []}))
                expected.add(manifest)
            selected, models = kit.model_files(store)
            self.assertEqual(selected, expected)
            self.assertEqual(set(models), set(kit.TAGS))

    def test_missing_or_corrupt_model_blocks_preparation(self):
        with tempfile.TemporaryDirectory() as directory:
            store = Path(directory)
            with self.assertRaises(FileNotFoundError):
                kit.model_files(store)
            manifest = store / "manifests-v2/ollama.com/library/gallery-guide-main/a01"
            manifest.parent.mkdir(parents=True)
            value = "sha256:" + "0" * 64
            manifest.write_text(json.dumps({"config": {"digest": value}, "layers": []}))
            blob = store / "blobs" / value.replace(":", "-")
            blob.parent.mkdir()
            blob.write_bytes(b"corrupt")
            with self.assertRaisesRegex(ValueError, "checksum"):
                kit.model_files(store)


if __name__ == "__main__":
    unittest.main()
