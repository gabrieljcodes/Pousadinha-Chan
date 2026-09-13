#!/usr/bin/env python3
"""
Populate gacha_booru_tags table in PostgreSQL using tags.sqlite.
Maps character names from gacha_characters to validated Gelbooru tags.
"""

import re
import sqlite3
import psycopg2
from psycopg2.extras import execute_values

DB_URL = "postgresql://postgres:postgres@127.0.0.1:5435/pousadinha"
SQLITE_PATH = "tags.sqlite"

def norm_tokens(s):
    if not s:
        return []
    return re.sub(r'[^a-z0-9 ]', '', s.lower()).split()

def main():
    print("Loading tags and aliases from tags.sqlite...")
    sqlite_conn = sqlite3.connect(SQLITE_PATH)
    scur = sqlite_conn.cursor()

    scur.execute('SELECT name, count FROM tags WHERE type = "character"')
    char_tags = {row[0]: row[1] for row in scur.fetchall()}

    scur.execute('SELECT alias, tag FROM tag_aliases')
    aliases = {row[0]: row[1] for row in scur.fetchall()}

    print(f"Loaded {len(char_tags)} character tags and {len(aliases)} aliases from sqlite.")

    # Index char_tags by base name
    tag_base_map = {}
    for tag, cnt in char_tags.items():
        m = re.match(r'^([a-z0-9_]+)_\((.+)\)$', tag)
        if m:
            base, series = m.group(1), m.group(2)
            tag_base_map.setdefault(base, []).append((tag, series, cnt))
        else:
            tag_base_map.setdefault(tag, []).append((tag, '', cnt))

    print("Loading active characters from PostgreSQL...")
    pg_conn = psycopg2.connect(DB_URL)
    pcur = pg_conn.cursor()

    pcur.execute("""
        SELECT c.id, c.name, c.favourites,
               COALESCE(string_agg(w.title, ' || '), '') as works
        FROM gacha_characters c
        LEFT JOIN gacha_character_works cw ON c.id = cw.character_id
        LEFT JOIN gacha_works w ON cw.work_id = w.id
        WHERE c.enabled
        GROUP BY c.id, c.name, c.favourites
        ORDER BY c.favourites DESC
    """)
    chars = pcur.fetchall()
    print(f"Loaded {len(chars)} enabled characters.")

    mapped_rows = []
    for cid, name, fav, works in chars:
        parts = norm_tokens(name)
        if not parts:
            continue

        cands = []
        if len(parts) == 1:
            cands.append(parts[0])
        elif len(parts) == 2:
            cands.extend([parts[0] + '_' + parts[1], parts[1] + '_' + parts[0]])
            # Hepburn normalization
            cands.append(parts[0].replace('ou', 'o').replace('uu', 'u') + '_' + parts[1].replace('ou', 'o').replace('uu', 'u'))
            cands.append(parts[1].replace('ou', 'o').replace('uu', 'u') + '_' + parts[0].replace('ou', 'o').replace('uu', 'u'))
        elif len(parts) >= 3:
            cands.append('_'.join(parts))
            cands.append(parts[-1] + '_' + '_'.join(parts[:-1]))
            if len(parts[-1]) == 1:
                cands.append(parts[1] + '_' + parts[2] + '._' + parts[0])
                cands.append(parts[0] + '_' + parts[1] + '_' + parts[2])
            if len(parts[1]) == 1:
                cands.append(parts[0] + '_' + parts[1] + '._' + parts[2])
                cands.append(parts[2] + '_' + parts[1] + '._' + parts[0])
                cands.append(parts[2] + '_' + parts[0] + '_' + parts[1])

        expanded = []
        for c in cands:
            expanded.append(c)
            if c in aliases:
                expanded.append(aliases[c])

        works_clean = set(norm_tokens(works))

        matched_tag = None
        # 1. Exact match in char_tags
        for cand in expanded:
            if cand in char_tags:
                matched_tag = cand
                break

        # 2. Base tag with series disambiguation
        if not matched_tag:
            for cand in expanded:
                if cand in tag_base_map:
                    matches = tag_base_map[cand]
                    if len(matches) == 1 and not matches[0][1]:
                        matched_tag = matches[0][0]
                        break
                    best_match, best_score = None, 0
                    for t, series, cnt in matches:
                        if not series:
                            score = 1
                        else:
                            series_words = set(series.split('_'))
                            score = len(series_words & works_clean) * 10
                        if score > best_score:
                            best_score, best_match = score, t
                    if best_match and best_score > 0:
                        matched_tag = best_match
                        break

        if matched_tag:
            mapped_rows.append((cid, 'gelbooru', matched_tag))

    print(f"Total characters successfully mapped: {len(mapped_rows)}")

    # Insert into PostgreSQL
    print("Upserting into gacha_booru_tags...")
    execute_values(
        pcur,
        """
        INSERT INTO gacha_booru_tags (character_id, provider, tag)
        VALUES %s
        ON CONFLICT (character_id, provider) DO UPDATE SET tag = EXCLUDED.tag
        """,
        mapped_rows,
        page_size=2000
    )
    pg_conn.commit()

    pcur.execute("SELECT count(*) FROM gacha_booru_tags WHERE provider = 'gelbooru'")
    total_tags_now = pcur.fetchone()[0]
    print(f"Total gelbooru tags registered in gacha_booru_tags: {total_tags_now}")

    pcur.close()
    pg_conn.close()
    sqlite_conn.close()
    print("Done!")

if __name__ == '__main__':
    main()
