#!/usr/bin/env python3
"""Rebuild C01 JSON offline from curated metadata and verified local JPEGs.

Writes a NEW output directory; never modifies the installed world. Python 3.9+
standard library only. No network, image synthesis, padding or resampling.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil

ROOT = Path(__file__).resolve().parents[1]
ARTISTS = {
    "guide_leonardo": (
        "Леонардо да Винчи", "Возрождение",
        "Спокойный исследователь и мастер наблюдения. Объясняй через форму, светотень и построение пространства. Предлагай маленький опыт: проследить край тени, сравнить планы, найти ось композиции.",
        "Как живопись Возрождения передаёт человека, объём и пространство; Леонардо и мастера его эпохи.",
    ),
    "guide_monet": (
        "Клод Моне", "Импрессионизм",
        "Внимательный наблюдатель изменчивого света. Начинай с конкретного участка картины, сравнивай цветные тени и отражения. Предлагай представить другой час дня; отличай этот мысленный опыт от факта о картине.",
        "Свет, атмосфера, отражения, сады и повторение мотивов в импрессионизме.",
    ),
    "guide_vangogh": (
        "Винсент ван Гог", "Постимпрессионизм",
        "Тёплый и увлечённый собеседник. Говори о выразительности цвета и ритме мазка; сначала спроси о впечатлении посетителя, затем свяжи его с видимыми деталями. Не ставь диагнозов и не романтизируй страдания.",
        "Выразительный цвет, линия, ритм и различные пути постимпрессионизма у Ван Гога, Гогена, Сезанна и Сёра.",
    ),
}


def write_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def vector(x, y, z):
    return {"x": x, "y": y, "z": z}


def pose(room_id, x):
    return {"room_id": room_id, "position": vector(x, 0, 6), "rotation": {"yaw": 0, "pitch": 0}}


def prepare(manifest_path, asset_root, output):
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if output.exists():
        raise ValueError("output must be a new directory; never overwrite a prepared world")
    # Verify every file before creating output. An interrupted copy can only leave
    # a new, incomplete directory; it cannot change the active content package.
    seen = set()
    room_ids = set()
    for room in manifest["rooms"]:
        if not re.fullmatch(r"[A-Za-z0-9_-]+", room["id"]) or room["id"] in room_ids:
            raise ValueError("invalid or repeated room ID")
        if room["guide_id"] not in ARTISTS:
            raise ValueError("unknown artist guide")
        room_ids.add(room["id"])
        for p in room["paintings"]:
            if not re.fullmatch(r"[A-Za-z0-9_-]+", p["id"]):
                raise ValueError("invalid painting ID")
    for room in manifest["rooms"]:
        for p in room["paintings"]:
            if p["file"] != f'assets/{p["id"]}.jpg' or p["id"] in seen:
                raise ValueError("invalid or repeated asset path")
            seen.add(p["id"])
            source = (asset_root / p["file"]).resolve()
            if not source.is_relative_to(asset_root.resolve()) or not source.is_file():
                raise ValueError("asset must be a regular file inside source root")
            data = source.read_bytes()
            if len(data) != p["bytes"] or hashlib.sha256(data).hexdigest() != p["sha256"]:
                raise ValueError(f'asset digest/size mismatch: {p["id"]}')
    output.mkdir(parents=True)
    catalog = {"world_id": manifest["world_id"], "world_version": manifest["world_version"],
               "rooms": [], "assets": [], "profiles": [],
               "start": pose(manifest["rooms"][0]["id"], 6), "spawns": {}}
    profiles = {}
    for guide_id, (name, era, style, topic) in ARTISTS.items():
        background = manifest.get("guide_background", {}).get(guide_id, {})
        profiles[guide_id] = {
            "personality": f"{name} — художественная реконструкция для образовательной галереи, а не реальный исторический собеседник. Представляешь раздел «{era}» и можешь обсуждать собственные картины и произведения других мастеров направления с явным указанием авторства. " + background.get("text", ""),
            "style": style, "topic": topic,
            "constraints": [
                "Обозначать художественную реконструкцию; говорить по-русски. Это учебная роль, не подлинные воспоминания художника.",
                "Не сочинять цитаты, встречи, биографические факты и намерения художников. При отсутствии фактов признавать ограниченность материалов.",
                "Все фактические утверждения, включая позу, цвет, родство и историю, опираются только на подготовленные факты и музейные описания. Если нужной детали нет, скажи, что данных недостаточно. Не достраивай их по памяти модели.",
                "Всегда различать автора картины и личность гида. Обсуждая другого автора, не говорить, что написал эту картину сам.",
                "Отделять музейные факты от наблюдения и интерпретации зрителя. Приблизительные датировки и атрибуции сохранять без ложной точности.",
                "Реконструкция может объяснять позднейшие учебные сравнения, но не выдаёт их за знание исторического человека при жизни.",
                "Учитывать только предоставленную личную историю посетителя. Не утверждать, что видишь пиксели или фотографию: доступны только текстовые материалы.",
                "Визуальные описания на английском переводи на русский без добавления деталей. Слова о настроении, характере и замысле допустимы только как явно обозначенная интерпретация, не вывод о реальном состоянии человека.",
                "Цвет экрана отличается от оригинала; не ставить медицинских диагнозов по картинам и не представлять эмоциональное впечатление историческим фактом.",
            ],
            "materials": [{
                "id": "shared_learning_goal", "text": manifest["educational_goal"] + " Сначала назови конкретную картину и её автора, затем предложи сравнить наблюдаемые детали с соседними работами. Итог экскурсии: посетитель обосновывает различия трёх художественных подходов примерами.",
                "sources": ["Авторская образовательная программа C01; интерпретация и упражнения не являются историческими цитатами."], "exhibit_ids": []
            }],
        }
        file = f"profiles/{guide_id}.json"
        catalog["profiles"].append({"id": guide_id, "file": file})
    rooms = manifest["rooms"]
    for i, curated in enumerate(rooms):
        rid = curated["id"]
        guide_id = curated["guide_id"]
        name, era, _, _ = ARTISTS[guide_id]
        room = {"id": rid, "world_version": manifest["world_version"],
                "title": curated["title"],
                "bounds": {"min": vector(0, 0, 0), "max": vector(12, 4, 12)},
                "surfaces": [], "doors": [], "exhibits": [],
                "guides": [{"id": guide_id, "display_name": name,
                            "subtitle": "Художественная реконструкция · " + era,
                            "position": vector(6, 0, 7.5), "rotation_y": 0,
                            "interaction_radius_m": 3}]}
        blocks = [("floor", (6, -0.1, 6), (12, 0.2, 12), "#C6AE89"),
                  ("north", (6, 2, 0), (12, 4, 0.2), "#F2EEE5"),
                  ("south", (6, 2, 12), (12, 4, 0.2), "#F2EEE5"),
                  ("west", (0, 2, 6), (0.2, 4, 12), "#E8E2D6"),
                  ("east", (12, 2, 6), (0.2, 4, 12), "#E8E2D6")]
        for sid, pos, size, color in blocks:
            room["surfaces"].append({"id": sid, "position": vector(*pos), "size": vector(*size), "color": color, "collidable": True})
        # A linear, reversible route; end rooms have one exit. Doors are usable
        # points in front of solid walls, not openings traversable by movement.
        for step, x, spawn_x in [(-1, 0.6, 10), (1, 11.4, 2)]:
            target_index = i + step
            if not 0 <= target_index < len(rooms):
                continue
            target = rooms[target_index]
            did = f"door_{i + 1:02d}_{target_index + 1:02d}"
            room["doors"].append({"id": did, "target_room_id": target["id"], "label": target["title"],
                                  "position": vector(x, 0, 6), "interaction_radius_m": 1.5})
            catalog["spawns"][did] = pose(target["id"], spawn_x)
        refs = []
        comparisons = []
        for j, p in enumerate(curated["paintings"]):
            exhibit_id = "exhibit_" + p["id"]
            refs.append(exhibit_id)
            visual_summary = p["visual_description"].split(". ", 1)[0] + "."
            comparisons.append(f'{p["artist"]}: «{p["title"]}», {p["date"]}. Наблюдаемое по NGA: {visual_summary}')
            # Preserve the source aspect ratio and fit each work in a 3 x 2.6 m
            # display area. Height is deliberately independent of image bytes.
            scale = min(3 / p["original_width"], 2.6 / p["original_height"])
            description = f'{p["artist"]}. {p["title"]}. Датировка музея: {p["date"]}. Техника: {p["medium"]}. {p["museum"]}, инв. {p["accession_number"]}. Репродукция: общественное достояние, NGA Open Access. Учебное сравнение: {curated["question"]}'
            room["exhibits"].append({"id": exhibit_id, "title": p["title"], "description": description,
                                     "image_asset_id": p["id"], "position": vector(2 + 4 * j, 1.9, 0.2), "rotation_y": 0,
                                     "width_m": round(p["original_width"] * scale, 6), "height_m": round(p["original_height"] * scale, 6), "interaction_radius_m": 3})
            catalog["assets"].append({"id": p["id"], "file": p["file"]})
            destination = output / p["file"]
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(asset_root / p["file"], destination)
            profiles[guide_id]["materials"].append({
                "id": "material_" + p["id"],
                "text": f'{p["artist"]}: «{p["title"]}» (музейное название: {p["original_title"]}). Датировка: {p["date"]}. Техника по музейной записи: {p["medium"]}. Музей: {p["museum"]}, инвентарный номер {p["accession_number"]}. Зал «{curated["title"]}». {curated["teaching_note"]} Вопрос посетителю: {curated["question"]} Музейное описание (NGA Visual Description; английский оригинал): {p["visual_description"]}',
                "sources": [p["object_url"], f'NGA Open Data, objectid={p["object_id"]}; локальная запись content/gallery.json; {manifest["source_dataset"]}'],
                "exhibit_ids": [exhibit_id],
            })
        profiles[guide_id]["materials"].append({"id": "lesson_" + rid,
            "text": f'Тема зала: {curated["title"]}. {curated["teaching_note"]} Учебное задание: {curated["question"]} Проверенные опорные сведения для сравнения: {"; ".join(comparisons)}',
            "sources": ["Авторское упражнение C01, не историческая цитата."] + [p["object_url"] for p in curated["paintings"]], "exhibit_ids": refs})
        file = f"rooms/{rid}.json"
        catalog["rooms"].append({"id": rid, "file": file})
        write_json(output / file, room)
    for gid, profile in profiles.items():
        background = manifest.get("guide_background", {}).get(gid)
        if background:
            profile["materials"].append({"id": "verified_background", "text": background["text"], "sources": background["sources"], "exhibit_ids": []})
        write_json(output / f"profiles/{gid}.json", profile)
    write_json(output / "catalog.json", catalog)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, default=ROOT / "content/gallery.json")
    parser.add_argument("--assets", type=Path, default=ROOT / "content/world", help="existing local world containing assets/")
    parser.add_argument("--output", type=Path, required=True, help="new output directory")
    args = parser.parse_args()
    prepare(args.manifest, args.assets, args.output)


if __name__ == "__main__":
    main()
