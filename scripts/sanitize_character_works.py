#!/usr/bin/env python3
"""
Sanitize and accurately rebuild gacha_character_works with strict 1-to-1 matching.

Context & Problem:
When characters were imported from the 202k MyAnimeList dataset, knownWorks
was mapped naively by lower(c.name). Because common names (Lucy, Rem, Levi,
Alice, Sakura, Shiro, Kagura, Haku, etc.) are shared by many distinct characters,
all characters sharing the same name inherited every work of every character with that name.
Additionally, 643 'Narrator' characters inherited 1,230 works each (~790,890 fake rows).

Solution:
Using the clean AniList ground-truth backup in pousadinha_backup_temp (4,717 characters,
24,682 genuine work links), we perform a 3-pass multi-signal matching algorithm:
  - Pass 1: Exact normalized name match + description/work/source disambiguation
  - Pass 2: Inverted-index token matching for name variations (e.g. 'Luffy Monkey' -> 'Luffy Monkey D.')
  - Pass 3: Native name (Kanji/Hangul) matching for romanization discrepancies

Rules & Invariants:
1. Strict 1-to-1 mapping: A production MAL character can only be claimed by AT MOST ONE backup persona.
2. For duplicate names, a match requires verifiable signal (work title match, description keyword overlap, or external_id match).
3. Heavy penalty for mismatched non-empty native names (e.g. 白 vs ハク).
4. Excludes AniList meta-character 'Narrator' (1,230 works).
5. Truncates and rebuilds pousadinha.gacha_character_works in a single atomic transaction.
"""

import re
import sys
import time
from collections import Counter
import psycopg2
from psycopg2.extras import execute_values

DB_PROD_URL = "postgresql://postgres:postgres@127.0.0.1:5435/pousadinha"
DB_BAK_URL = "postgresql://postgres:postgres@127.0.0.1:5435/pousadinha_backup_temp"

STOPWORDS = {
    'the', 'and', 'his', 'her', 'for', 'are', 'has', 'was', 'with', 'that', 'from', 'this',
    'have', 'been', 'which', 'they', 'their', 'when', 'after', 'about', 'first', 'other',
    'into', 'more', 'some', 'only', 'would', 'could', 'should', 'character', 'anime', 'series',
    'manga', 'who', 'him', 'she', 'not', 'but', 'all', 'one', 'two', 'also', 'out', 'over', 'now',
    'will', 'can', 'even', 'its', 'than', 'them', 'then', 'there', 'what', 'such', 'were', 'any',
    'see', 'time', 'like', 'just', 'well', 'how', 'down', 'up'
}

TITLE_STOPWORDS = {'season', 'part', 'movie', 'special', 'specials', 'series', 'animation', 'the'}

def word_set(s: str) -> set:
    if not s:
        return set()
    return set(re.findall(r'[a-zA-Z]{3,}', s.lower())) - STOPWORDS

def norm_name(s: str) -> str:
    s = re.sub(r'\s+', ' ', s.lower().strip())
    s = re.sub(r'-(sama|san|kun|chan)$', '', s)
    return s

def clean_title_words(title: str) -> list:
    cleaned = re.sub(r'[^a-z0-9 ]', '', title.lower())
    return [w for w in cleaned.split() if len(w) > 3 and w not in TITLE_STOPWORDS]

def score_candidate(binfo, cand):
    score = 0.0
    work_titles = [w[2] for w in binfo['works']]
    work_matched = False
    for wt in work_titles:
        if wt.lower() in cand['desc_lower']:
            score += 250.0
            work_matched = True
        else:
            words = clean_title_words(wt)
            if words and any(wd in cand['desc_lower'] for wd in words):
                score += 100.0
                work_matched = True

    shared_words = binfo['words'] & cand['words']
    score += len(shared_words) * 20.0

    nat_matched = False
    if binfo['nat'] and cand['nat']:
        if binfo['nat'] == cand['nat']:
            score += 120.0
            nat_matched = True
        else:
            score -= 200.0

    ext_matched = False
    if str(cand['id']) == str(binfo['ext']):
        score += 150.0
        ext_matched = True

    if cand['fav'] > 0:
        score += min(40.0, cand['fav'] / 500.0)

    return score, work_matched, len(shared_words), nat_matched, ext_matched

def main():
    start_time = time.time()
    print("Connecting to databases...")
    conn_prod = psycopg2.connect(DB_PROD_URL)
    cur_prod = conn_prod.cursor()

    conn_bak = psycopg2.connect(DB_BAK_URL)
    cur_bak = conn_bak.cursor()

    print("Loading clean ground-truth characters & works from pousadinha_backup_temp...")
    cur_bak.execute("""
        SELECT c.id, c.name, c.native_name, cs.external_id, c.favourites, c.description,
               cw.work_id, cw.role, w.title
        FROM gacha_characters c
        JOIN gacha_character_sources cs ON c.id = cs.character_id
        JOIN gacha_character_works cw ON c.id = cw.character_id
        JOIN gacha_works w ON cw.work_id = w.id
    """)
    bak_rows = cur_bak.fetchall()
    bak_chars = {}
    for bid, bname, bnat, bext, bfav, bdesc, wid, role, wtitle in bak_rows:
        if bid not in bak_chars:
            bak_chars[bid] = {
                'id': bid,
                'name': bname,
                'norm_name': norm_name(bname),
                'nat': (bnat or '').strip(),
                'ext': bext,
                'fav': bfav,
                'desc': bdesc or '',
                'words': word_set(bdesc or ''),
                'works': []
            }
        bak_chars[bid]['works'].append((wid, role, wtitle))

    bak_name_counts = Counter(b['norm_name'] for b in bak_chars.values())
    print(f"Loaded {len(bak_chars)} backup characters across {len(bak_name_counts)} distinct names.")

    print("Loading production characters from pousadinha...")
    cur_prod.execute("SELECT id, name, native_name, favourites, description FROM gacha_characters")
    prod_by_name = {}
    prod_by_token = {}
    prod_by_nat = {}

    for cid, name, nat, fav, desc in cur_prod.fetchall():
        lname = norm_name(name)
        clean_nat = (nat or '').strip()
        item = {
            'id': cid,
            'name': lname,
            'nat': clean_nat,
            'fav': fav or 0,
            'desc': desc or '',
            'desc_lower': (desc or '').lower(),
            'words': word_set(desc or '')
        }
        prod_by_name.setdefault(lname, []).append(item)
        for tok in lname.split():
            if len(tok) >= 3:
                prod_by_token.setdefault(tok, []).append(item)
        if clean_nat:
            prod_by_nat.setdefault(clean_nat, []).append(item)

    matched_map = {}
    assigned_prod = set()

    # Pass 1: Exact normalized name match with candidate scoring
    proposals1 = []
    for bid, binfo in bak_chars.items():
        if binfo['norm_name'] == 'narrator' and len(binfo['works']) > 100:
            continue
        cands = prod_by_name.get(binfo['norm_name'], [])
        if not cands:
            continue

        is_duplicate = (bak_name_counts[binfo['norm_name']] > 1) or (len(cands) > 1)
        if not is_duplicate:
            proposals1.append((1000.0, bid, cands[0]['id']))
            continue

        scored = []
        for cand in cands:
            s, w_match, shared_cnt, nat_match, ext_match = score_candidate(binfo, cand)
            # For duplicate names, require at least one positive disambiguation signal
            if (w_match or shared_cnt >= 1 or ext_match) and s > 0:
                scored.append((s, cand['id']))

        if scored:
            scored.sort(key=lambda x: x[0], reverse=True)
            proposals1.append((scored[0][0], bid, scored[0][1]))

    proposals1.sort(key=lambda x: x[0], reverse=True)
    for score, bid, pid in proposals1:
        if bid not in matched_map and pid not in assigned_prod:
            matched_map[bid] = pid
            assigned_prod.add(pid)

    p1_count = len(matched_map)
    print(f"Pass 1 (Exact normalized name) matched: {p1_count}")

    # Pass 2: Token match with inverted index
    proposals2 = []
    for bid, binfo in bak_chars.items():
        if bid in matched_map or (binfo['norm_name'] == 'narrator' and len(binfo['works']) > 100):
            continue

        bname_parts = [p for p in binfo['norm_name'].split() if len(p) >= 3]
        if not bname_parts:
            continue

        candidate_set = {}
        for tok in bname_parts:
            for cand in prod_by_token.get(tok, []):
                if cand['id'] not in assigned_prod:
                    candidate_set[cand['id']] = cand
        if not candidate_set:
            continue

        scored = []
        for cid, cand in candidate_set.items():
            cand_tokens = set(cand['name'].split())
            shared = set(bname_parts) & cand_tokens
            if not shared:
                continue
            s, w_match, shared_cnt, nat_match, ext_match = score_candidate(binfo, cand)
            s += len(shared) * 30.0
            if (w_match or shared_cnt >= 1 or ext_match or nat_match) and s >= 100.0:
                scored.append((s, cand['id']))

        if scored:
            scored.sort(key=lambda x: x[0], reverse=True)
            proposals2.append((scored[0][0], bid, scored[0][1]))

    proposals2.sort(key=lambda x: x[0], reverse=True)
    for score, bid, pid in proposals2:
        if bid not in matched_map and pid not in assigned_prod:
            matched_map[bid] = pid
            assigned_prod.add(pid)

    p2_count = len(matched_map) - p1_count
    print(f"Pass 2 (Token match) matched: {p2_count}")

    # Pass 3: Native name match
    proposals3 = []
    for bid, binfo in bak_chars.items():
        if bid in matched_map or not binfo['nat'] or (binfo['norm_name'] == 'narrator' and len(binfo['works']) > 100):
            continue

        cands = [c for c in prod_by_nat.get(binfo['nat'], []) if c['id'] not in assigned_prod]
        if not cands:
            continue

        scored = []
        for cand in cands:
            s, w_match, shared_cnt, nat_match, ext_match = score_candidate(binfo, cand)
            if (w_match or shared_cnt >= 1 or ext_match or cand['fav'] > 100) and s > 0:
                scored.append((s, cand['id']))

        if scored:
            scored.sort(key=lambda x: x[0], reverse=True)
            proposals3.append((scored[0][0], bid, scored[0][1]))

    proposals3.sort(key=lambda x: x[0], reverse=True)
    for score, bid, pid in proposals3:
        if bid not in matched_map and pid not in assigned_prod:
            matched_map[bid] = pid
            assigned_prod.add(pid)

    p3_count = len(matched_map) - p1_count - p2_count
    print(f"Pass 3 (Native name) matched: {p3_count}")

    print(f"Total matched characters: {len(matched_map)} / {len(bak_chars)} ({len(matched_map)/len(bak_chars)*100:.1f}%)")
    assert len(matched_map) == len(set(matched_map.values())), "Collision error: multiple backup chars mapped to same MAL ID!"

    # Build clean relations list
    clean_rels = set()
    for bid, prod_id in matched_map.items():
        for wid, role, _ in bak_chars[bid]['works']:
            clean_rels.add((prod_id, wid, role))

    print(f"Total unique clean character-work pairs to insert: {len(clean_rels)}")

    # Verification assertions
    # 1. Lucy (Cyberpunk) must be 213159
    assert matched_map.get(200) == 213159, f"Lucy Cyberpunk expected 213159, got {matched_map.get(200)}"
    # 2. Lucy (Elfen Lied) must be 738
    assert matched_map.get(747) == 738, f"Lucy Elfen Lied expected 738, got {matched_map.get(747)}"
    # 3. Rem (Re:Zero) must be 118763
    assert matched_map.get(27) == 118763, f"Rem Re:Zero expected 118763, got {matched_map.get(27)}"
    # 4. Rem (Death Note) must be 1905
    assert matched_map.get(1626) == 1905, f"Rem Death Note expected 1905, got {matched_map.get(1626)}"
    # 5. Levi (Attack on Titan) must be 45627
    assert matched_map.get(24) == 45627, f"Levi AoT expected 45627, got {matched_map.get(24)}"
    # 6. Guts (Berserk) must be 422
    assert matched_map.get(13) == 422, f"Guts Berserk expected 422, got {matched_map.get(13)}"
    # 7. Haku (Naruto) must be 2039
    assert matched_map.get(1673) == 2039, f"Haku Naruto expected 2039, got {matched_map.get(1673)}"
    # 8. Haku (Utawarerumono) must be 129765
    assert matched_map.get(3544) == 129765, f"Haku Utawarerumono expected 129765, got {matched_map.get(3544)}"
    # 9. Haku (Spirited Away) must be 385
    assert matched_map.get(553) == 385, f"Haku Spirited Away expected 385, got {matched_map.get(553)}"
    # 10. Uta (Tokyo Ghoul) must be 93343
    assert matched_map.get(770) == 93343, f"Uta Tokyo Ghoul expected 93343, got {matched_map.get(770)}"
    # 11. Uta (One Piece Film Red) must be 209380
    assert matched_map.get(1475) == 209380, f"Uta OP Film Red expected 209380, got {matched_map.get(1475)}"

    print("All strict sanity checks passed! Executing database transaction...")

    # Atomic transaction
    cur_prod.execute("TRUNCATE gacha_character_works;")
    execute_values(
        cur_prod,
        "INSERT INTO gacha_character_works (character_id, work_id, role) VALUES %s ON CONFLICT (character_id, work_id) DO NOTHING",
        list(clean_rels),
        page_size=2000
    )
    conn_prod.commit()
    print("Database transaction committed successfully!")

    # Verify post-sanitation statistics
    cur_prod.execute("SELECT count(*) FROM gacha_character_works")
    total_works_now = cur_prod.fetchone()[0]
    print(f"Post-sanitation gacha_character_works row count: {total_works_now}")

    cur_prod.close()
    conn_prod.close()
    cur_bak.close()
    conn_bak.close()

    print(f"\nAll operations completed in {time.time() - start_time:.2f}s!")

if __name__ == '__main__':
    main()
