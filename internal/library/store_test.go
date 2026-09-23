package library

import (
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestUpsertAndGet(t *testing.T) {
	st := openTestStore(t)
	it, err := st.UpsertItem(LibraryItem{
		SourcePath: "/s/game.iso", ContentHash: "h1", Platform: PlatformPS2,
		Title: "Game", SizeBytes: 123, Status: StatusNew,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if it.ID == 0 {
		t.Error("no ID assigned")
	}
	got, err := st.GetByHash("h1")
	if err != nil || got == nil {
		t.Fatalf("GetByHash: %v %+v", err, got)
	}
	if got.Title != "Game" || got.Status != StatusNew {
		t.Errorf("round-trip = %+v", got)
	}
	if missing, err := st.GetByHash("nope"); err != nil || missing != nil {
		t.Errorf("missing hash = %+v, %v", missing, err)
	}
	gotByID, err := st.Get(it.ID)
	if err != nil || gotByID == nil || gotByID.ContentHash != "h1" {
		t.Errorf("Get = %+v, %v", gotByID, err)
	}
	if missing, err := st.Get(9999); err != nil || missing != nil {
		t.Errorf("missing id = %+v, %v", missing, err)
	}
}

func TestUpsertPreservesDetection(t *testing.T) {
	st := openTestStore(t)
	first, err := st.UpsertItem(LibraryItem{
		SourcePath: "/s/game.iso", ContentHash: "h1", Platform: PlatformPS2,
		Title: "Game", SizeBytes: 123, Status: StatusError,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetDiscOverride("h1", DiscDVD); err != nil {
		t.Fatal(err)
	}
	// Re-scan with a moved file: override survives, error reopens as new.
	second, err := st.UpsertItem(LibraryItem{
		SourcePath: "/t/game.iso", ContentHash: "h1", Platform: PlatformPS2,
		Title: "Game", SizeBytes: 123, Status: StatusNew,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Error("re-upsert created a second row")
	}
	if second.DiscType != DiscDVD || second.DetectionMethod != MethodOverride {
		t.Errorf("override clobbered: %+v", second)
	}
	if second.SourcePath != "/t/game.iso" {
		t.Errorf("path not refreshed: %q", second.SourcePath)
	}
	// Error status reopens; non-error statuses are kept as-is.
	if second.Status != StatusNew {
		t.Errorf("status = %q, want new", second.Status)
	}
}

func TestOverrideValidation(t *testing.T) {
	st := openTestStore(t)
	if err := st.SetDiscOverride("missing", DiscCD); err == nil {
		t.Error("expected error for unknown hash")
	}
	if _, err := st.UpsertItem(LibraryItem{SourcePath: "/s/g.iso",
		ContentHash: "h1", Platform: PlatformPS2, Title: "G"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetDiscOverride("h1", DiscType("bluray")); err == nil {
		t.Error("expected error for bad disc type")
	}
	if ov, ok, _ := st.GetOverride("h1"); ok || ov != "" {
		t.Errorf("no override yet, got %q,%v", ov, ok)
	}
}

func TestUpdateDetectionRespectsOverride(t *testing.T) {
	st := openTestStore(t)
	it, _ := st.UpsertItem(LibraryItem{SourcePath: "/s/g.iso",
		ContentHash: "h1", Platform: PlatformPS2, Title: "G"})
	if err := st.UpdateDetection(it.ID, DiscCD, MethodHeuristic); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetByHash("h1")
	if got.DiscType != DiscCD || got.DetectionMethod != MethodHeuristic {
		t.Errorf("detection not recorded: %+v", got)
	}
	if err := st.SetDiscOverride("h1", DiscDVD); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateDetection(it.ID, DiscCD, MethodInspected); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetByHash("h1")
	if got.DiscType != DiscDVD || got.DetectionMethod != MethodOverride {
		t.Errorf("override clobbered: %+v", got)
	}
}

func TestListAndGroups(t *testing.T) {
	st := openTestStore(t)
	g1, _ := st.NextGroupID()
	g := g1
	if _, err := st.UpsertItem(LibraryItem{SourcePath: "/a.iso",
		ContentHash: "a", Platform: PlatformPS2, Title: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertItem(LibraryItem{SourcePath: "/b.iso",
		ContentHash: "b", Platform: PlatformPS2, Title: "B", DiscGroupID: &g}); err != nil {
		t.Fatal(err)
	}
	list, err := st.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %d, %v", len(list), err)
	}
	if list[0].SourcePath != "/a.iso" || list[1].SourcePath != "/b.iso" {
		t.Errorf("order wrong: %+v", list)
	}
	if list[1].DiscGroupID == nil || *list[1].DiscGroupID != g1 {
		t.Errorf("group not persisted: %+v", list[1])
	}
	g2, _ := st.NextGroupID()
	if g2 != g1+1 {
		t.Errorf("group IDs %d then %d, want sequential", g1, g2)
	}
	if g1 < 1 {
		t.Errorf("first group ID = %d, want >= 1", g1)
	}
	if err := st.RemapGroup(g1, 99); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetByHash("b")
	if got.DiscGroupID == nil || *got.DiscGroupID != 99 {
		t.Errorf("remap failed: %+v", got)
	}
}

func TestListByGroupID(t *testing.T) {
	st := openTestStore(t)
	g := int64(7)
	if _, err := st.UpsertItem(LibraryItem{SourcePath: "/a", ContentHash: "a",
		Platform: PlatformPS1, Title: "G", DiscIndex: 2, DiscGroupID: &g}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertItem(LibraryItem{SourcePath: "/b", ContentHash: "b",
		Platform: PlatformPS1, Title: "G", DiscIndex: 1, DiscGroupID: &g}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertItem(LibraryItem{SourcePath: "/c", ContentHash: "c",
		Platform: PlatformPS1, Title: "Solo"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListByGroupID(7)
	if err != nil || len(got) != 2 {
		t.Fatalf("group = %v, %v", got, err)
	}
	if got[0].DiscIndex != 1 || got[1].DiscIndex != 2 {
		t.Errorf("order = %+v", got)
	}
	empty, err := st.ListByGroupID(999)
	if err != nil || len(empty) != 0 {
		t.Errorf("missing group = %v, %v", empty, err)
	}
}
