package library

// ImportDir scans root, persists new items (preserving overrides and
// reopening error rows), remaps temporary multi-disc group IDs to real
// ones, and runs PS2 disc-type detection, recording each decision. It is
// the shared implementation behind `oplbm import` and POST
// /api/library/import.
func ImportDir(st *Store, root string, db *TitleDB) ([]LibraryItem, error) {
	items, err := ScanDir(root)
	if err != nil {
		return nil, err
	}
	tempGroups := map[int64]bool{}
	stored := make([]LibraryItem, 0, len(items))
	for _, it := range items {
		saved, err := st.UpsertItem(it)
		if err != nil {
			return nil, err
		}
		if saved.DiscGroupID != nil && *saved.DiscGroupID < 0 {
			tempGroups[*saved.DiscGroupID] = true
		}
		stored = append(stored, saved)
	}
	remap := map[int64]int64{}
	for tmp := range tempGroups {
		real, err := st.NextGroupID()
		if err != nil {
			return nil, err
		}
		if err := st.RemapGroup(tmp, real); err != nil {
			return nil, err
		}
		remap[tmp] = real
	}
	for i, it := range stored {
		if it.DiscGroupID != nil {
			if real, ok := remap[*it.DiscGroupID]; ok {
				g := real
				stored[i].DiscGroupID = &g
			} else {
				cur, err := st.Get(it.ID)
				if err != nil {
					return nil, err
				}
				stored[i].DiscGroupID = cur.DiscGroupID
			}
		}
		if it.Platform != PlatformPS2 {
			continue
		}
		dt, m, err := Detect(&stored[i], st, db)
		if err != nil {
			return nil, err
		}
		if err := st.UpdateDetection(stored[i].ID, dt, m); err != nil {
			return nil, err
		}
		stored[i].DiscType, stored[i].DetectionMethod = dt, m
		// GameID extraction (spec §2.3.5): streaming SYSTEM.CNF -> BOOT2.
		// No GameID is not an error; games still transfer, just without
		// enrichment (art/cheats). Uncertain flag is persisted for M5.
		if gid, uncertain, err := ExtractGameID(stored[i].SourcePath); err == nil {
			_ = st.UpdateGameID(stored[i].ID, gid, uncertain)
			stored[i].GameID, stored[i].GameIDUncertain = gid, uncertain
		}
	}
	return stored, nil
}
