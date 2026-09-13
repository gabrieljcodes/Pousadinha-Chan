#!/usr/bin/env python3
"""
scripts/reclassify_nb_and_other.py

Re-evaluates all characters currently marked as 'Non-binary' or 'Other'
using refined thresholds on SmilingWolf WD14 ViT Tagger v3.
Fixes false Non-binary (like Kurapika, Grell, Ryuuji) and human characters
that were caught in 'Other'.
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

os.environ["PYTORCH_CUDA_ALLOC_CONF"] = "expandable_segments:True"

DB_HOST = os.environ.get("DB_HOST", "localhost")
DB_PORT = int(os.environ.get("DB_PORT", "5435"))
DB_USER = os.environ.get("DB_USER", "postgres")
DB_PASS = os.environ.get("DB_PASS", "postgres")
DB_NAME = os.environ.get("DB_NAME", "pousadinha")

MODEL_PATH = "models/wd-vit-tagger-v3/model.onnx"
BASE_DATA_DIR = "data/gacha/"
MAL_PLACEHOLDER_SIZE = 1801

BATCH_SIZE = 32
PREFETCH_BATCHES = 2
DB_COMMIT_INTERVAL = 250

TAGS = {
    '1girl': 4,
    'multiple_girls': 19,
    '1boy': 25,
    'multiple_boys': 106,
    'no_humans': 307,
    'androgynous': 790,
    'ambiguous_gender': 2829
}

stop_requested = False

def handle_signal(sig, frame):
    global stop_requested
    print("\n[!] Stopping gracefully...")
    stop_requested = True

signal.signal(signal.SIGINT, handle_signal)
signal.signal(signal.SIGTERM, handle_signal)

def load_and_preprocess_image(path):
    full_path = os.path.join(BASE_DATA_DIR, path)
    try:
        # If it's the known MAL placeholder, skip loading
        if os.path.getsize(full_path) == MAL_PLACEHOLDER_SIZE:
            return None, True  # (arr=None, is_placeholder=True)

        img = Image.open(full_path).convert('RGB')
        w, h = img.size
        scale = 448 / max(w, h)
        new_w, new_h = int(w * scale), int(h * scale)
        resized = img.resize((new_w, new_h), Image.BICUBIC)
        padded = Image.new('RGB', (448, 448), (255, 255, 255))
        padded.paste(resized, ((448 - new_w) // 2, (448 - new_h) // 2))
        arr = np.array(padded)[:, :, ::-1].astype(np.float32)
        return arr, False
    except Exception:
        return None, False

def classify_refined(p_girl, p_boy, p_nohuman, p_nb):
    # 1. Non-human (animal, creature, mascot, robot)
    if p_nohuman >= 0.50 and p_girl < 0.30 and p_boy < 0.30:
        return 'Other'

    # 2. Definite Non-binary / Ambiguous (Rimuru, Phospho, Antarcticite, Diamond, Cinnabar, Najimi)
    # High NB tag without an overwhelming boy or girl tag
    if p_nb >= 0.60 and max(p_boy, p_girl) < 0.85:
        return 'Non-binary'

    # 3. Clear Male (Kurapika, Grell, Haku, Astolfo, Ryuuji)
    if p_boy >= 0.50 and p_boy > p_girl + 0.10:
        return 'Male'

    # 4. Clear Female (Nelliel, Yosano, Motoko)
    if p_girl >= 0.50 and p_girl > p_boy + 0.10:
        return 'Female'

    # 5. Moderate Non-binary / Genderless (Nanachi, Neferpitou, Enkidu)
    if p_nb >= 0.30 and max(p_boy, p_girl) < 0.40:
        return 'Non-binary'

    # 6. Both boy and girl detected, or moderate scores
    if p_boy >= 0.30 and p_boy > p_girl:
        return 'Male'
    if p_girl >= 0.30 and p_girl > p_boy:
        return 'Female'

    # 7. Fallback non-human / other
    if p_nohuman >= 0.30:
        return 'Other'

    return 'Other'

def main():
    start_time = time.time()
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

    cur.execute("""
        SELECT c.id, c.name, a.path, c.gender
        FROM gacha_characters c
        JOIN gacha_assets a ON a.character_id = c.id
        WHERE c.gender IN ('Non-binary', 'Other') AND a.path IS NOT NULL AND a.path <> ''
        ORDER BY c.id ASC;
    """)
    all_rows = cur.fetchall()
    total_target = len(all_rows)
    print(f"Total characters to evaluate in Non-binary + Other: {total_target:,}")

    if total_target == 0:
        print("No characters to re-classify.")
        cur.close()
        conn.close()
        return

    print(f"Loading WD14 ViT Tagger v3 from {MODEL_PATH} onto CUDA...")
    torch.backends.cuda.matmul.allow_tf32 = True
    torch.backends.cudnn.allow_tf32 = True
    model = convert(MODEL_PATH).to('cuda:0').eval()
    print("Model loaded.")

    item_queue = queue.Queue(maxsize=PREFETCH_BATCHES)

    def load_single(item):
        cid, name, path, old_g = item
        arr, is_ph = load_and_preprocess_image(path)
        return (cid, name, arr, old_g, is_ph)

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
    transition_stats = Counter()
    final_stats = Counter()
    last_print_time = time.time()

    print("\nStarting re-classification loop...")
    print("-" * 80)

    try:
        while not stop_requested:
            batch_items = item_queue.get()
            if batch_items is None:
                break

            # Separate into GPU candidates vs placeholders/failed
            needs_gpu = [(cid, name, arr, old_g) for cid, name, arr, old_g, is_ph in batch_items if arr is not None]
            no_gpu = [(cid, name, old_g) for cid, name, arr, old_g, is_ph in batch_items if arr is None]

            if needs_gpu:
                cids = [item[0] for item in needs_gpu]
                old_genders = [item[3] for item in needs_gpu]
                
                def run_inference(sub_items):
                    tensors = torch.from_numpy(np.stack([item[2] for item in sub_items])).to('cuda:0', non_blocking=True)
                    with torch.inference_mode(), torch.autocast(device_type='cuda', dtype=torch.float16):
                        return model(tensors).float().cpu().numpy()

                try:
                    probs = run_inference(needs_gpu)
                except torch.OutOfMemoryError:
                    torch.cuda.empty_cache()
                    # Run in smaller slices of 8
                    probs_list = []
                    for k in range(0, len(needs_gpu), 8):
                        probs_list.append(run_inference(needs_gpu[k:k+8]))
                    probs = np.concatenate(probs_list, axis=0)

                for j, cid in enumerate(cids):
                    p_girl = max(probs[j][TAGS['1girl']], probs[j][TAGS['multiple_girls']])
                    p_boy = max(probs[j][TAGS['1boy']], probs[j][TAGS['multiple_boys']])
                    p_nh = probs[j][TAGS['no_humans']]
                    p_nb = max(probs[j][TAGS['androgynous']], probs[j][TAGS['ambiguous_gender']])

                    new_gender = classify_refined(p_girl, p_boy, p_nh, p_nb)
                    old_gender = old_genders[j]
                    final_stats[new_gender] += 1

                    if new_gender != old_gender:
                        transition_stats[f"{old_gender} -> {new_gender}"] += 1
                        updates_buffer.append((new_gender, cid))

            # Placeholders and missing images stay 'Other'
            for cid, name, old_g in no_gpu:
                final_stats['Other'] += 1
                if old_g != 'Other':
                    transition_stats[f"{old_g} -> Other"] += 1
                    updates_buffer.append(('Other', cid))

            processed_count += len(batch_items)

            if len(updates_buffer) >= DB_COMMIT_INTERVAL:
                psycopg2.extras.execute_batch(
                    cur,
                    "UPDATE gacha_characters SET gender = %s WHERE id = %s",
                    updates_buffer,
                    page_size=500
                )
                conn.commit()
                updates_buffer.clear()

            now = time.time()
            if now - last_print_time >= 2.0 or processed_count == total_target:
                elapsed = now - start_time
                speed = processed_count / elapsed if elapsed > 0 else 0
                pct = (processed_count / total_target) * 100
                rem_sec = (total_target - processed_count) / speed if speed > 0 else 0

                sys.stdout.write(
                    f"\r[{processed_count:,}/{total_target:,} ({pct:.1f}%)] "
                    f"Speed: {speed:.1f} img/s | ETA: {int(rem_sec//60)}m{int(rem_sec%60):02d}s | "
                    f"Changes: {sum(transition_stats.values()):,} (NB->M: {transition_stats.get('Non-binary -> Male', 0)}, NB->F: {transition_stats.get('Non-binary -> Female', 0)}, Other->M: {transition_stats.get('Other -> Male', 0)}, Other->F: {transition_stats.get('Other -> Female', 0)}) "
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
            updates_buffer.clear()

        print("\n" + "-" * 80)
        print("Re-classification of Non-binary and Other completed successfully!")

    except Exception as e:
        print(f"\n[Error]: {e}")
        conn.rollback()
        raise
    finally:
        cur.close()
        conn.close()

    total_time = time.time() - start_time
    print(f"Elapsed: {total_time:.1f}s ({total_time/60:.2f} minutes)")
    print("\nSummary of Transitions:")
    for k, v in sorted(transition_stats.items(), key=lambda x: -x[1]):
        print(f"  {k:30}: {v:,}")
    print("\nResulting Distribution in evaluated pool:")
    for k, v in final_stats.items():
        print(f"  {k:15}: {v:,}")

if __name__ == '__main__':
    main()
