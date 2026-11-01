"""Filesystem regression tests for the offline C01 authoring tool."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location("prepare_gallery", Path(__file__).with_name("prepare-gallery.py"))
PREPARE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PREPARE)


class PrepareGalleryTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.assets = self.root / "source"
        (self.assets / "assets").mkdir(parents=True)
        original = json.loads((PREPARE.ROOT / "content/gallery.json").read_text())
        self.manifest = copy.deepcopy(original)
        self.manifest["rooms"] = self.manifest["rooms"][:1]
        for p in self.manifest["rooms"][0]["paintings"]:
            data = p["id"].encode()
            (self.assets / p["file"]).write_bytes(data)
            p["bytes"] = len(data)
            p["sha256"] = hashlib.sha256(data).hexdigest()
        self.path = self.root / "source.json"
        self.output = self.root / "output"

    def prepare(self):
        self.path.write_text(json.dumps(self.manifest))
        PREPARE.prepare(self.path, self.assets, self.output)

    def test_reject_room_traversal_before_writing(self):
        victim = self.root / "victim.json"
        victim.write_text("preserve")
        self.manifest["rooms"][0]["id"] = "../../victim"
        with self.assertRaises(ValueError):
            self.prepare()
        self.assertEqual(victim.read_text(), "preserve")
        self.assertFalse(self.output.exists())

    def test_reject_asset_traversal_before_reading(self):
        p = self.manifest["rooms"][0]["paintings"][0]
        p["id"] = "../../victim"
        p["file"] = "assets/../../victim.jpg"
        with self.assertRaises(ValueError):
            self.prepare()
        self.assertFalse(self.output.exists())

    def test_reject_hash_mismatch_before_writing(self):
        self.manifest["rooms"][0]["paintings"][0]["sha256"] = "0" * 64
        with self.assertRaises(ValueError):
            self.prepare()
        self.assertFalse(self.output.exists())

    def test_reject_existing_output_without_modification(self):
        self.output.mkdir()
        sentinel = self.output / "catalog.json"
        sentinel.write_text("preserve")
        with self.assertRaises(ValueError):
            self.prepare()
        self.assertEqual(sentinel.read_text(), "preserve")

    def test_deterministic_local_rebuild(self):
        self.prepare()
        first = {p.relative_to(self.output): p.read_bytes() for p in self.output.rglob("*") if p.is_file()}
        self.output = self.root / "second"
        self.prepare()
        second = {p.relative_to(self.output): p.read_bytes() for p in self.output.rglob("*") if p.is_file()}
        self.assertEqual(first, second)
        self.assertIn(Path("catalog.json"), first)
        room = self.manifest["rooms"][0]
        rebuilt = json.loads(first[Path("rooms") / (room["id"] + ".json")])
        self.assertEqual(rebuilt["title"], room["title"])
        self.assertTrue(rebuilt["title"].startswith("01. "))
        self.assertEqual(rebuilt["world_version"], "gallery_c01_v2")


if __name__ == "__main__":
    unittest.main()
