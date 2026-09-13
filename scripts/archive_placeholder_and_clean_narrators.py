#!/usr/bin/env python3
"""
Archive characters with MAL placeholder photo and delete photo-less Narrators.

Tasks:
1. Delete all 'Narrator' characters that have the MAL placeholder photo (or no photo):
   - Excludes them completely from gacha_characters, gacha_assets, gacha_character_sources.
   - Keeps all 32 Narrators that have real/distinct photos (e.g. Ten no Koe, ID 4068).
2. Archive all other characters that have the blue MAL placeholder photo:
   - Sets gacha_characters: archived_at = now(), enabled = false, auto_publish_blocked = true
   - Sets gacha_assets: status = 'rejected', is_primary = false, archived_at = now()
   - Keeps them safely stored in the database archive, removing them from active rolls & catalog.
"""

import os
import time
import psycopg2

DB_URL = "postgresql://postgres:postgres@127.0.0.1:5435/pousadinha"
MAL_PLACEHOLDER_SHA256 = "e7a155a13c237aab72142c9a51c743e6f5e9748836dde31944a98a90d0c6fa9a"

def main():
    t0 = time.time()
    conn = psycopg2.connect(DB_URL)
    cur = conn.cursor()

    print("=== 1. Identifying Narrators with placeholder photo ===")
    cur.execute("""
        SELECT c.id, c.name, a.path
        FROM gacha_characters c
        JOIN gacha_assets a ON c.id = a.character_id
        WHERE lower(c.name) LIKE '%%narrator%%'
          AND (a.source_url LIKE '%%apple-touch-icon%%' OR a.sha256 = %s)
    """, (MAL_PLACEHOLDER_SHA256,))
    narrators_to_delete = cur.fetchall()
    print(f"Found {len(narrators_to_delete)} photo-less Narrator characters to delete.")

    cur.execute("""
        SELECT c.id, c.name, c.favourites, a.source_url
        FROM gacha_characters c
        JOIN gacha_assets a ON c.id = a.character_id
        WHERE lower(c.name) LIKE '%%narrator%%'
          AND a.source_url NOT LIKE '%%apple-touch-icon%%' AND a.sha256 != %s
    """, (MAL_PLACEHOLDER_SHA256,))
    narrators_to_keep = cur.fetchall()
    print(f"Found {len(narrators_to_keep)} Narrator characters with real/different photos to KEEP.")
    for n in narrators_to_keep[:5]:
        print(f"  Keeping: ID {n[0]} - {n[1]} ({n[2]} favs)")

    print("\n=== 2. Identifying general characters with MAL placeholder photo ===")
    cur.execute("""
        SELECT c.id
        FROM gacha_characters c
        JOIN gacha_assets a ON c.id = a.character_id
        WHERE lower(c.name) NOT LIKE '%%narrator%%'
          AND (a.source_url LIKE '%%apple-touch-icon%%' OR a.sha256 = %s)
    """, (MAL_PLACEHOLDER_SHA256,))
    characters_to_archive = [r[0] for r in cur.fetchall()]
    print(f"Found {len(characters_to_archive)} general characters to ARCHIVE.")

    # Begin atomic transaction
    print("\n=== 3. Executing Database Updates in Atomic Transaction ===")
    narrator_ids = [n[0] for n in narrators_to_delete]
    if narrator_ids:
        # Delete Narrator child relations first
        cur.execute("DELETE FROM gacha_character_works WHERE character_id = ANY(%s)", (narrator_ids,))
        cur.execute("DELETE FROM gacha_character_sources WHERE character_id = ANY(%s)", (narrator_ids,))
        cur.execute("DELETE FROM gacha_assets WHERE character_id = ANY(%s)", (narrator_ids,))
        cur.execute("DELETE FROM gacha_characters WHERE id = ANY(%s)", (narrator_ids,))
        print(f"Deleted {len(narrator_ids)} photo-less Narrator characters from database.")

    if characters_to_archive:
        # Archive assets
        cur.execute("""
            UPDATE gacha_assets
            SET status = 'rejected',
                is_primary = false,
                archived_at = now()
            WHERE character_id = ANY(%s)
        """, (characters_to_archive,))
        
        # Archive characters
        cur.execute("""
            UPDATE gacha_characters
            SET archived_at = now(),
                enabled = false,
                auto_publish_blocked = true,
                updated_at = now()
            WHERE id = ANY(%s)
        """, (characters_to_archive,))
        print(f"Archived {len(characters_to_archive)} characters and their assets in database.")

    conn.commit()
    print("Database transaction committed successfully!")

    # Clean up local dummy files for deleted narrators
    deleted_files = 0
    for _, _, rel_path in narrators_to_delete:
        if rel_path:
            full_path = os.path.join("data/gacha", rel_path)
            if os.path.exists(full_path):
                try:
                    os.remove(full_path)
                    deleted_files += 1
                except Exception as ex:
                    pass
    print(f"Cleaned up {deleted_files} local placeholder files for deleted narrators.")

    # Post-validation
    print("\n=== 4. Post-Update Validation ===")
    cur.execute("SELECT count(*) FROM gacha_characters WHERE enabled")
    active_count = cur.fetchone()[0]
    print(f"Active & Enabled Characters: {active_count}")

    cur.execute("SELECT count(*) FROM gacha_characters WHERE archived_at IS NOT NULL")
    archived_count = cur.fetchone()[0]
    print(f"Archived Characters: {archived_count}")

    cur.execute("SELECT count(*) FROM gacha_characters WHERE lower(name) LIKE '%%narrator%%'")
    remaining_narrators = cur.fetchone()[0]
    print(f"Remaining Narrator characters (all with real photos): {remaining_narrators}")

    cur.execute("""
        SELECT count(*)
        FROM gacha_characters c
        JOIN gacha_assets a ON c.id = a.character_id
        WHERE c.enabled AND (a.source_url LIKE '%%apple-touch-icon%%' OR a.sha256 = %s)
    """, (MAL_PLACEHOLDER_SHA256,))
    active_placeholders = cur.fetchone()[0]
    print(f"Active characters with MAL placeholder photo: {active_placeholders} (Must be 0!)")
    assert active_placeholders == 0, "Error: active characters still have placeholder photos!"

    cur.close()
    conn.close()
    print(f"\nAll operations completed successfully in {time.time() - t0:.2f}s!")

if __name__ == '__main__':
    main()
