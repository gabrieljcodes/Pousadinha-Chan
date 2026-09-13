#!/usr/bin/env python3
"""
scripts/classify_genders_wd14.py

Classifies ALL characters in PostgreSQL using SmilingWolf's WD14 ViT Tagger v3
running on NVIDIA RTX GPU with CUDA + FP16 autocast.
Re-evaluates every character that has an image to eliminate previous regex errors.
"""

import sys
import os
import time
import signal
import queue
import threading
from collections import Counter
from PIL import Image
import numpy as np
import psycopg2
import psycopg2.extras
import torch
from onnx2torch import convert
import concurrent.futures

# Database Configuration
DB_HOST = os.environ.get("DB_HOST", "localhost")
DB_PORT = int(os.environ.get("DB_PORT", "5435"))
DB_USER = os.environ.get("DB_USER", "postgres")
DB_PASS = os.environ.get("DB_PASS", "postgres")
DB_NAME = os.environ.get("DB_NAME", "pousadinha")

MODEL_PATH = "models/wd-vit-tagger-v3/model.onnx"
BASE_DATA_DIR = "data/gacha/"
CHECKPOINT_FILE = "data/gacha/gender_classifier_checkpoint.txt"

BATCH_SIZE = 64
PREFETCH_BATCHES = 4
DB_COMMIT_INTERVAL = 500

# Tag Indices for WD14 ViT Tagger v3
TAGS = {
    '1girl': 4,
    'multiple_girls': 19,
    '1boy': 25,
    'multiple_boys': 106,
    'no_humans': 307,
    'androgynous': 790,
    'ambiguous_gender': 2829,
    'otoko_no_ko': 643
}

stop_requested = False

def handle_signal(sig, frame):
    global stop_requested
    print("\n[!] Graceful stop requested. Finishing current batch and saving checkpoint...")
    stop_requested = True

signal.signal(signal.SIGINT, handle_signal)
signal.signal(signal.SIGTERM, handle_signal)

def load_and_preprocess_image(path):
    full_path = os.path.join(BASE_DATA_DIR, path)
    try:
        img = Image.open(full_path).convert('RGB')
        w, h = img.size
        scale = 448 / max(w, h)
        new_w, new_h = int(w * scale), int(h * scale)
        resized = img.resize((new_w, new_h), Image.BICUBIC)
        padded = Image.new('RGB', (448, 448), (255, 255, 255))
        padded.paste(resized, ((448 - new_w) // 2, (448 - new_h) // 2))
        # BGR format, float32 (0.0 to 255.0)
        arr = np.array(padded)[:, :, ::-1].astype(np.float32)
        return arr
    except Exception:
        return None

def classify_gender(p_girl, p_boy, p_nohuman, p_nb):
    # 1. Non-human (animal, creature, mecha, robot, mascot)
    if p_nohuman >= 0.40 and p_girl < 0.35 and p_boy < 0.35:
        return 'Other'

    # 2. Canonical androgynous / non-binary
    if p_nb >= 0.30:
        return 'Non-binary'

    # 3. Both boy and girl detected closely (genuine ambiguous)
    if p_girl >= 0.50 and p_boy >= 0.50 and abs(p_girl - p_boy) < 0.05:
        return 'Non-binary'

    # 4. Female
    if p_girl >= 0.30 and p_girl > p_boy:
        return 'Female'

    # 5. Male
    if p_boy >= 0.30 and p_boy > p_girl:
        return 'Male'

    # 6. Fallback non-human / Other
    if p_nohuman >= 0.25:
        return 'Other'

    return 'Other'

def read_checkpoint():
    if os.path.exists(CHECKPOINT_FILE):
        try:
            with open(CHECKPOINT_FILE, 'r') as f:
                val = f.read().strip()
                if val:
                    return int(val)
        except Exception as e:
            print(f"Warning reading checkpoint: {e}")
    return 0

def write_checkpoint(last_id):
    tmp = CHECKPOINT_FILE + ".tmp"
    with open(tmp, 'w') as f:
        f.write(str(last_id))
    os.replace(tmp, CHECKPOINT_FILE)

def main():
    start_time = time.time()
    last_id = read_checkpoint()

    print(f"Connecting to database {DB_USER}@{DB_HOST}:{DB_PORT}/{DB_NAME}...")
    conn = psycopg2.connect(
        host=DB_HOST,
        port=DB_PORT,
        user=DB_USER,
        password=DB_PASS,
        dbname=DB_NAME
    )
    conn.autocommit = False
    cur = conn.cursor()

    # Query total records with valid images
    cur.execute("""
        SELECT count(*)
        FROM gacha_characters c
        JOIN gacha_assets a ON a.character_id = c.id
        WHERE c.id > %s AND a.path IS NOT NULL AND a.path <> ''
    """, (last_id,))
    total_remaining = cur.fetchone()[0]

    cur.execute("""
        SELECT count(*)
        FROM gacha_characters c
        JOIN gacha_assets a ON a.character_id = c.id
        WHERE a.path IS NOT NULL AND a.path <> ''
    """)
    total_all = cur.fetchone()[0]

    print(f"Total characters with images: {total_all:,} | Resuming from ID > {last_id} ({total_remaining:,} remaining)")

    if total_remaining == 0:
        print("All characters already processed!")
        cur.close()
        conn.close()
        return

    print(f"Loading WD14 ViT Tagger v3 from {MODEL_PATH} onto CUDA...")
    torch.backends.cuda.matmul.allow_tf32 = True
    torch.backends.cudnn.allow_tf32 = True
    model = convert(MODEL_PATH).to('cuda:0').eval()
    print("Model successfully loaded on NVIDIA GPU.")

    print("Fetching character metadata from Postgres...")
    cur.execute("""
        SELECT c.id, c.name, a.path
        FROM gacha_characters c
        JOIN gacha_assets a ON a.character_id = c.id
        WHERE c.id > %s AND a.path IS NOT NULL AND a.path <> ''
        ORDER BY c.id ASC
    """, (last_id,))
    all_rows = cur.fetchall()
    print(f"Fetched {len(all_rows):,} records to process.")

    item_queue = queue.Queue(maxsize=PREFETCH_BATCHES)

    def load_single(item):
        cid, name, path = item
        arr = load_and_preprocess_image(path)
        return (cid, name, arr)

    def prefetch_worker():
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            for i in range(0, len(all_rows), BATCH_SIZE):
                if stop_requested:
                    break
                batch_slice = all_rows[i:i + BATCH_SIZE]
                loaded_items = list(pool.map(load_single, batch_slice))
                item_queue.put(loaded_items)
        item_queue.put(None)

    prefetch_thread = threading.Thread(target=prefetch_worker, daemon=True)
    prefetch_thread.start()

    processed_count = 0
    updates_buffer = []
    stats = Counter()
    current_last_id = last_id
    last_print_time = time.time()

    print("\nStarting full classification loop (RTX 4060 + FP16 autocast)...")
    print("-" * 80)

    try:
        while not stop_requested:
            batch_items = item_queue.get()
            if batch_items is None:
                break

            valid_batch = [(cid, name, arr) for cid, name, arr in batch_items if arr is not None]
            failed_batch = [(cid, name) for cid, name, arr in batch_items if arr is None]

            if valid_batch:
                cids = [item[0] for item in valid_batch]
                tensors = torch.from_numpy(np.stack([item[2] for item in valid_batch])).to('cuda:0', non_blocking=True)

                with torch.inference_mode(), torch.autocast(device_type='cuda', dtype=torch.float16):
                    # Direct model output (already sigmoid probabilities, NO extra sigmoid!)
                    probs = model(tensors).float().cpu().numpy()

                for j, cid in enumerate(cids):
                    p_girl = max(probs[j][TAGS['1girl']], probs[j][TAGS['multiple_girls']])
                    p_boy = max(probs[j][TAGS['1boy']], probs[j][TAGS['multiple_boys']])
                    p_nohuman = probs[j][TAGS['no_humans']]
                    p_nb = max(probs[j][TAGS['androgynous']], probs[j][TAGS['ambiguous_gender']])

                    gender = classify_gender(p_girl, p_boy, p_nohuman, p_nb)
                    stats[gender] += 1
                    updates_buffer.append((gender, cid))

            for cid, _ in failed_batch:
                stats['Other'] += 1
                updates_buffer.append(('Other', cid))

            current_last_id = batch_items[-1][0]
            processed_count += len(batch_items)

            if len(updates_buffer) >= DB_COMMIT_INTERVAL:
                psycopg2.extras.execute_batch(
                    cur,
                    "UPDATE gacha_characters SET gender = %s WHERE id = %s",
                    updates_buffer,
                    page_size=500
                )
                conn.commit()
                write_checkpoint(current_last_id)
                updates_buffer.clear()

            now = time.time()
            if now - last_print_time >= 2.0 or processed_count == len(all_rows):
                elapsed = now - start_time
                speed = processed_count / elapsed if elapsed > 0 else 0
                pct = (processed_count / len(all_rows)) * 100
                remaining_sec = (len(all_rows) - processed_count) / speed if speed > 0 else 0
                eta_m = int(remaining_sec // 60)
                eta_s = int(remaining_sec % 60)

                sys.stdout.write(
                    f"\r[{processed_count:,}/{len(all_rows):,} ({pct:.1f}%)] "
                    f"Speed: {speed:.1f} img/s | ETA: {eta_m}m{eta_s:02d}s | "
                    f"Female: {stats['Female']:,} | Male: {stats['Male']:,} | NB: {stats['Non-binary']:,} | Other: {stats['Other']:,}  "
                )
                sys.stdout.flush()
                last_print_time = now

        if updates_buffer:
            psycopg2.extras.execute_batch(
                cur,
                "UPDATE gacha_characters SET gender = %s WHERE id = %s",
                updates_buffer,
                page_size=500
            )
            conn.commit()
            write_checkpoint(current_last_id)
            updates_buffer.clear()

        print("\n" + "-" * 80)
        if stop_requested:
            print(f"[!] Paused at character ID {current_last_id}. Checkpoint saved.")
        else:
            print("All characters successfully classified!")
            if os.path.exists(CHECKPOINT_FILE):
                os.remove(CHECKPOINT_FILE)

    except Exception as e:
        print(f"\n[Error during classification]: {e}")
        conn.rollback()
        raise
    finally:
        cur.close()
        conn.close()

    total_time = time.time() - start_time
    print(f"\nTotal elapsed: {total_time/60:.1f} minutes")
    print(f"Final Batch Stats: {dict(stats)}")

if __name__ == '__main__':
    main()
