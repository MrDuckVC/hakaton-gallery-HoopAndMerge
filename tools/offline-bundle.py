#!/usr/bin/env python3
"""Prepare/check a self-contained Linux/WSL demo kit without copying private data."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

REPO = Path(__file__).resolve().parents[1]
TAGS = ("gallery-guide-main:a01", "gallery-guide-reserve:a01")


def digest(file):
    with file.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def model_files(store):
    """Include only the two prepared manifests and their referenced blobs."""
    selected = set()
    models = {}
    for tag in TAGS:
        name, version = tag.split(":")
        manifest = Path("manifests-v2/ollama.com/library") / name / version
        document = json.loads((store / manifest).read_text())
        models[tag] = "sha256:" + digest(store / manifest)
        selected.add(manifest)
        for layer in [document["config"], *document["layers"]]:
            value = layer["digest"]
            if not re.fullmatch(r"sha256:[a-f0-9]{64}", value):
                raise ValueError("invalid model layer digest")
            blob = Path("blobs") / value.replace(":", "-")
            if digest(store / blob) != value.removeprefix("sha256:"):
                raise ValueError("model blob checksum mismatch")
            selected.add(blob)
            metadata = Path("metadata") / (blob.name + ".json")
            if (store / metadata).is_file():
                selected.add(metadata)
    return selected, models


def inventory(root):
    result = {}
    for file in sorted(root.rglob("*")):
        if file.is_symlink():
            # Runtime library symlinks are permitted only inside the kit.
            file.resolve(strict=True).relative_to(root.resolve())
            result[file.relative_to(root).as_posix()] = {"symlink": os.readlink(file)}
        elif file.is_file() and file != root / "bundle-manifest.json":
            result[file.relative_to(root).as_posix()] = {"sha256": digest(file)}
    return result


def verify(root):
    manifest = json.loads((root / "bundle-manifest.json").read_text())
    actual = inventory(root)
    if actual != manifest["files"]:
        changed = sorted(k for k in actual.keys() | manifest["files"].keys()
                         if actual.get(k) != manifest["files"].get(k))
        raise ValueError("bundle differs from manifest: " + ", ".join(changed[:10]))
    print(f"Verified {len(actual)} artifacts; no downloads performed.")


def check_runtime(runtime):
    # Force an unreachable loopback port: --version must report the client binary,
    # not the version of a daemon already running on the operator's laptop.
    result = subprocess.run([str(runtime / "bin/ollama"), "--version"],
                            env={**os.environ, "OLLAMA_HOST": "127.0.0.1:1"},
                            capture_output=True, text=True, timeout=10, check=False)
    version = re.search(r"client version is ([0-9.]+)", result.stdout + result.stderr)
    if result.returncode != 0 or version is None or version.group(1) != "0.40.13":
        raise ValueError("Ollama client runtime must be version 0.40.13")
    return version.group(1)


def kit_environment(template, photo_dir):
    # Keep the original files when using the same database from another cwd.
    absolute = str(photo_dir.resolve())
    if any(char in absolute for char in "'\x00\r\n"):
        raise ValueError("photo directory cannot contain quotes or control characters")
    template = re.sub(r"^FRONTEND_DIST_DIR=.*$", "FRONTEND_DIST_DIR=./frontend", template, flags=re.MULTILINE)
    return re.sub(r"^PHOTO_DIR=.*$", lambda _: "PHOTO_DIR='" + absolute + "'", template, flags=re.MULTILINE)


def prepare(root, dist, runtime, store, photo_dir=None):
    # Refuse existing outputs before doing any work; never merge into a live kit.
    if root.exists() or root.is_symlink():
        raise ValueError("output exists; choose a new directory")
    for file in (dist / "index.html", dist / "build-info.json", REPO / "bin/gallery",
                 REPO / "bin/migrate", REPO / "bin/admin", runtime / "bin/ollama"):
        if not file.is_file():
            raise ValueError(f"missing prerequisite: {file}")
    build = json.loads((dist / "build-info.json").read_text())
    if build.get("mock") is not False or not re.fullmatch(r"[a-f0-9]{40}", build.get("commit", "")):
        raise ValueError("frontend must have production build provenance (mock=false, commit)")
    if any(file.is_symlink() or file.name.startswith(".") for file in dist.rglob("*")):
        raise ValueError("dist must contain only public files, without hidden files/symlinks")
    runtime_version = check_runtime(runtime)
    selected, models = model_files(store)
    template = kit_environment((REPO / ".env.example").read_text(), photo_dir or REPO / "data/photos")
    compose = (REPO / "docker-compose.yml").read_text()
    image = re.search(r"^\s+image: (postgres:[\w.-]+)$", compose, re.MULTILINE).group(1)
    image_id = subprocess.check_output(["docker", "image", "inspect", image,
                                       "--format", "{{.Id}}"], text=True).strip()
    root.mkdir(parents=True)
    # A failed preparation leaves an incomplete new output for inspection, never a valid manifest.
    for source, target in ((REPO / "bin/gallery", "bin/gallery"),
                           (REPO / "bin/migrate", "bin/migrate"),
                           (REPO / "bin/admin", "bin/admin"),
                           (REPO / "docker-compose.yml", "docker-compose.yml"),
                           (REPO / ".env.example", ".env.example")):
        destination = root / target
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, destination)
    for source, target in ((dist, "frontend"), (runtime, "ollama-runtime"),
                           (REPO / "content/world", "content/world"),
                           (REPO / "migrations", "migrations"), (REPO / "docs", "docs"),
                           (REPO / "configs/ollama", "configs/ollama")):
        shutil.copytree(source, root / target, symlinks=True)
    shutil.copy2(REPO / "tools/offline-bundle.py", root / "verify-bundle.py")
    for relative in sorted(selected):
        destination = root / "models" / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(store / relative, destination)
    subprocess.run(["docker", "image", "save", "--output", str(root / "postgres.tar"), image], check=True)
    # The template contains placeholders only, never the operator's actual .env.
    (root / ".env.example").write_text(template)
    metadata = {
        "format": 1, "postgres_image": image, "postgres_image_id": image_id,
        "ollama_version": runtime_version, "models": models, "frontend": build,
        "backend_base_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=REPO, text=True).strip(),
        "backend_worktree_diff_sha256": hashlib.sha256(subprocess.check_output(["git", "diff", "HEAD"], cwd=REPO)).hexdigest(),
        "world_version": json.loads((root / "content/world/catalog.json").read_text())["world_version"],
        "files": inventory(root),
    }
    (root / "bundle-manifest.json").write_text(json.dumps(metadata, indent=2, ensure_ascii=False) + "\n")
    print(f"Prepared {root}; private .env, database data and photographs were not copied.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    pack = commands.add_parser("prepare", help="online preparation, requires already built/downloaded prerequisites")
    pack.add_argument("--output", type=Path, required=True)
    pack.add_argument("--frontend-dist", type=Path, required=True)
    pack.add_argument("--runtime", type=Path, default=REPO / "bin/ollama-runtime")
    pack.add_argument("--photo-dir", type=Path, default=REPO / "data/photos", help="persistent photo directory to reference, never copied")
    pack.add_argument("--models", type=Path, default=REPO / "data/ollama/models")
    check = commands.add_parser("verify", help="read-only verification, no Docker/network required")
    check.add_argument("directory", type=Path)
    args = parser.parse_args()
    try:
        if args.command == "prepare":
            prepare(args.output.resolve(), args.frontend_dist.resolve(), args.runtime.resolve(), args.models.resolve(), args.photo_dir.resolve())
        else:
            verify(args.directory.resolve())
    except (OSError, ValueError, KeyError, AttributeError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
        print(f"Offline kit: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
