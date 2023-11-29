INSERT INTO reaction_per_livestream SELECT l.id AS livestream_id, COUNT(*) AS reaction_count FROM livestreams l L
EFT JOIN reactions r ON l.id = r.livestream_id GROUP BY l.id;
