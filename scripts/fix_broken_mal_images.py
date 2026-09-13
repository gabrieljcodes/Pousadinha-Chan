#!/usr/bin/env python3
"""
scripts/fix_broken_mal_images.py

Fixes broken and missing character photos:
1. Queries characters where path IS NULL or path = ''.
2. Tries to retrieve current image from MAL character page: https://myanimelist.net/character/<id>
3. If page is Invalid/deleted or has questionmark placeholder, searches MAL character API.
4. Fallback: Searches Gelbooru for characters with no photo on MAL.
5. Downloads the image locally into data/gacha/mal/<shard>/<id>.<ext>.
6. Runs SmilingWolf WD14 tagger on RTX 4060 to determine gender.
7. Updates gacha_assets and gacha_characters in PostgreSQL.
"""

import sys
import os
import re
import json
import time
import urllib.request
import urllib.parse
from PIL import Image
import numpy as np
import psycopg2
import psycopg2.extras
import torch
from onnx2torch import convert
import concurrent.futures

os.environ["PYTORCH_CUDA_ALLOC_CONF"] = "expandable_segments:True"

DB_HOST = os.environ.get("DB_HOST", "localhost")
DB_PORT = int(os.environ.get("DB_PORT", "5435"))
DB_USER = os.environ.get("DB_USER", "postgres")
DB_PASS = os.environ.get("DB_PASS", "postgres")
DB_NAME = os.environ.get("DB_NAME", "pousadinha")

MODEL_PATH = "models/wd-vit-tagger-v3/model.onnx"
BASE_DATA_DIR = "data/gacha/"

GELBOORU_API_KEY = os.environ.get("GELBOORU_API_KEY", "502c447596534b933e31d061fff46480c25cd47583dacd4762c8a3f350111989")
GELBOORU_USER_ID = os.environ.get("GELBOORU_USER_ID", "1464208")

HEADERS = {
    'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36',
    'Referer': 'https://myanimelist.net/'
}

TAGS = {
    '1girl': 4,
    'multiple_girls': 19,
    '1boy': 25,
    'multiple_boys': 106,
    'no_humans': 307,
    'androgynous': 790,
    'ambiguous_gender': 2829
}

def clean_tag(name):
    # Convert character name to booru format: "Kurisu Makise" -> "makise_kurisu"
    parts = re.findall(r'[a-zA-Z0-9]+', name.lower())
    if len(parts) >= 2:
        # Check Japanese vs Western order or try both
        return f"{parts[1]}_{parts[0]}"
    return "_".join(parts)

def fetch_image_from_mal(cid, name):
    # 1. Direct page
    url = f"https://myanimelist.net/character/{cid}"
    req = urllib.request.Request(url, headers=HEADERS)
    try:
        with urllib.request.urlopen(req, timeout=8) as resp:
            html = resp.read().decode('utf-8', errors='ignore')
            if 'Invalid ID provided' not in html:
                m = re.search(r'<meta property="og:image" content="(https://cdn\.myanimelist\.net/images/characters/[^"]+)"', html)
                if m and 'questionmark' not in m.group(1):
                    return m.group(1), 'mal_direct'
                
                # Try gallery on pics page
                pics_url = f"https://myanimelist.net/character/{cid}/pics"
                pics_req = urllib.request.Request(pics_url, headers=HEADERS)
                try:
                    with urllib.request.urlopen(pics_req, timeout=8) as presp:
                        phtml = presp.read().decode('utf-8', errors='ignore')
                        pm = re.search(r'https://cdn\.myanimelist\.net/images/characters/[0-9]+/[0-9]+\.jpg', phtml)
                        if pm and 'questionmark' not in pm.group(0):
                            return pm.group(0), 'mal_pics'
                except Exception:
                    pass
    except Exception:
        pass

    # 2. MAL search prefix API
    q = urllib.parse.quote(name)
    search_url = f"https://myanimelist.net/search/prefix.json?type=character&keyword={q}"
    sreq = urllib.request.Request(search_url, headers=HEADERS)
    try:
        with urllib.request.urlopen(sreq, timeout=8) as resp:
            data = json.loads(resp.read().decode('utf-8'))
            for cat in data.get('categories', []):
                for item in cat.get('items', []):
                    img = item.get('image_url')
                    if img and 'questionmark' not in img:
                        return img, f"mal_search_{item.get('id')}"
    except Exception:
        pass

    # 3. Gelbooru Fallback (if character has a reasonable name)
    tag = clean_tag(name)
    if tag:
        gurl = f"https://gelbooru.com/index.php?page=dapi&s=post&q=index&json=1&api_key={GELBOORU_API_KEY}&user_id={GELBOORU_USER_ID}&tags={urllib.parse.quote(tag)}+rating:general+sort:score&limit=1"
        greq = urllib.request.Request(gurl, headers=HEADERS)
        try:
            with urllib.request.urlopen(greq, timeout=8) as resp:
                gdata = json.loads(resp.read().decode('utf-8'))
                posts = gdata.get('post', [])
                if posts and len(posts) > 0:
                    furl = posts[0].get('file_url')
                    if furl:
                        return furl, 'gelbooru'
        except Exception:
            pass

    return None, None

def classify_gender(p_girl, p_boy, p_nohuman, p_nb):
    if p_nohuman >= 0.50 and p_girl < 0.30 and p_boy < 0.30:
        return 'Other'
    if p_nb >= 0.60 and max(p_boy, p_girl) < 0.85:
        return 'Non-binary'
    if p_boy >= 0.50 and p_boy > p_girl + 0.10:
        return 'Male'
    if p_girl >= 0.50 and p_girl > p_boy + 0.10:
        return 'Female'
    if p_nb >= 0.30 and max(p_boy, p_girl) < 0.40:
        return 'Non-binary'
    if p_boy >= 0.30 and p_boy > p_girl:
        return 'Male'
    if p_girl >= 0.30 and p_girl > p_boy:
        return 'Female'
    return 'Other'

def main():
    print(f"Connecting to database {DB_USER}@{DB_HOST}:{DB_PORT}/{DB_NAME}...")
    conn = psycopg2.connect(
        host=DB_HOST,
        port=DB_PORT,
        user=DB_USER,
        password=DB_PASS,
        dbname=DB_NAME
    )
    conn.autocommit = True
    cur = conn.cursor()

    cur.execute("""
        SELECT c.id, c.name, a.source_url, c.favourites
        FROM gacha_characters c
        LEFT JOIN gacha_assets a ON a.character_id = c.id
        WHERE a.path IS NULL OR a.path = ''
        ORDER BY c.favourites DESC;
    """)
    rows = cur.fetchall()
    print(f"Found {len(rows):,} characters without a local photo.")

    if len(rows) == 0:
        print("All characters already have photos!")
        cur.close()
        conn.close()
        return

    print(f"Loading WD14 ViT Tagger onto CUDA...")
    model = convert(MODEL_PATH).to('cuda:0').eval()
    print("Model ready on RTX 4060.")

    recovered = 0
    failed = 0

    print("\nStarting search & download pipeline...")
    print("-" * 80)

    for i, (cid, name, old_url, favs) in enumerate(rows, 1):
        found_url, source_type = fetch_image_from_mal(cid, name)

        if not found_url:
            failed += 1
            sys.stdout.write(f"\r[{i}/{len(rows)}] Recovered: {recovered} | Failed: {failed} | Last: {name[:20]} (not found)  ")
            sys.stdout.flush()
            time.sleep(0.15)
            continue

        # Download image
        shard = str(cid // 1000)
        ext = ".jpg"
        if ".png" in found_url.lower(): ext = ".png"
        elif ".webp" in found_url.lower(): ext = ".webp"

        rel_path = f"mal/{shard}/{cid}{ext}"
        local_path = os.path.join(BASE_DATA_DIR, rel_path)
        os.makedirs(os.path.dirname(local_path), exist_ok=True)

        try:
            dreq = urllib.request.Request(found_url, headers=HEADERS)
            with urllib.request.urlopen(dreq, timeout=10) as resp, open(local_path, 'wb') as f:
                f.write(resp.read())

            img = Image.open(local_path).convert('RGB')
            w, h = img.size
            scale = 448 / max(w, h)
            nw, nh = int(w * scale), int(h * scale)
            r = img.resize((nw, nh), Image.BICUBIC)
            pad = Image.new('RGB', (448, 448), (255, 255, 255))
            pad.paste(r, ((448 - nw) // 2, (448 - nh) // 2))
            arr = np.array(pad)[:, :, ::-1].astype(np.float32)

            with torch.inference_mode(), torch.autocast(device_type='cuda', dtype=torch.float16):
                probs = model(torch.from_numpy(arr).unsqueeze(0).to('cuda:0'))[0].float().cpu().numpy()

            p_girl = max(probs[TAGS['1girl']], probs[TAGS['multiple_girls']])
            p_boy = max(probs[TAGS['1boy']], probs[TAGS['multiple_boys']])
            p_nh = probs[TAGS['no_humans']]
            p_nb = max(probs[TAGS['androgynous']], probs[TAGS['ambiguous_gender']])
            gender = classify_gender(p_girl, p_boy, p_nh, p_nb)

            # Update DB
            mtype = 'image/png' if ext == '.png' else ('image/webp' if ext == '.webp' else 'image/jpeg')
            cur.execute("""
                UPDATE gacha_characters SET gender = %s WHERE id = %s;
                UPDATE gacha_assets 
                SET path = %s, source_url = %s, status = 'approved', media_type = %s, width = %s, height = %s 
                WHERE character_id = %s;
            """, (gender, cid, rel_path, found_url, mtype, w, h, cid))

            recovered += 1
            sys.stdout.write(f"\r[{i}/{len(rows)}] Recovered: {recovered} | Failed: {failed} | Fixed: {name[:20]} -> {gender} ({source_type})  ")
            sys.stdout.flush()

        except Exception as e:
            failed += 1
            if os.path.exists(local_path):
                try: os.remove(local_path)
                except: pass

        time.sleep(0.15)

    print("\n" + "-" * 80)
    print(f"Finished! Total Processed: {len(rows):,} | Successfully Recovered: {recovered:,} | Unobtainable: {failed:,}")
    cur.close()
    conn.close()

if __name__ == '__main__':
    main()
