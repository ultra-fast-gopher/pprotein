package collect

import (
	"bytes"
	"testing"

	"github.com/ultra-fast-gopher/pprotein/internal/storage"
)

func TestAddedSnapshotPersistsInTypeBucket(t *testing.T) {
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"slowlog", "pgslowlog"} {
		snapshot := newSnapshot(store, typ, ".log", &SnapshotTarget{Label: typ})
		if err := snapshot.Add([]byte("log bytes\n")); err != nil {
			t.Fatal(err)
		}
		raw, err := store.Get(typ, snapshot.ID)
		if err != nil || len(raw) == 0 {
			t.Fatalf("stored %s snapshot: raw=%q, err=%v", typ, raw, err)
		}
		var restored Snapshot
		if err := restored.unmarshal(raw); err != nil {
			t.Fatal(err)
		}
		if restored.Type != typ || restored.ID != snapshot.ID || restored.Label != typ {
			t.Fatalf("restored metadata does not match: %+v", restored)
		}
		bodyPath, err := snapshot.BodyPath()
		if err != nil || bodyPath == "" {
			t.Fatalf("body path = %q, err=%v", bodyPath, err)
		}
		entries, err := store.GetAll(typ)
		if err != nil || len(entries) != 1 || !bytes.Equal(entries[0], raw) {
			t.Fatalf("restored %s index: entries=%q, err=%v", typ, entries, err)
		}
	}
}
