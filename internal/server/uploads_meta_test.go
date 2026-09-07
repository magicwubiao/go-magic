package server

import (
	"testing"
)

func TestUploadsMetaCRUD(t *testing.T) {
	magicHome := t.TempDir()
	st, err := openUploadsMeta(magicHome)
	if err != nil {
		t.Fatalf("openUploadsMeta: %v", err)
	}
	defer st.Close()

	if _, ok := st.Get("sess1", "abc-uuid.pdf"); ok {
		t.Fatal("expected no metadata before insert")
	}

	if err := st.Upsert("sess1", "abc-uuid.pdf", "年度报告.pdf", 1024, "application/pdf"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	row, ok := st.Get("sess1", "abc-uuid.pdf")
	if !ok {
		t.Fatal("expected metadata after insert")
	}
	if row.OrigName != "年度报告.pdf" || row.Size != 1024 || row.MIME != "application/pdf" {
		t.Fatalf("unexpected row: %+v", row)
	}

	// Upsert is idempotent per (session, disk).
	if err := st.Upsert("sess1", "abc-uuid.pdf", "年度报告-v2.pdf", 2048, "application/pdf"); err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	row, _ = st.Get("sess1", "abc-uuid.pdf")
	if row.OrigName != "年度报告-v2.pdf" || row.Size != 2048 {
		t.Fatalf("upsert should refresh: %+v", row)
	}

	// Same disk name under another session stays distinct.
	if err := st.Upsert("sess2", "abc-uuid.pdf", "other.pdf", 10, ""); err != nil {
		t.Fatalf("upsert other session: %v", err)
	}
	if r, _ := st.Get("sess1", "abc-uuid.pdf"); r.OrigName != "年度报告-v2.pdf" {
		t.Fatalf("session isolation broken: %+v", r)
	}

	// DeleteDisk removes one row only.
	if err := st.DeleteDisk("sess2", "abc-uuid.pdf"); err != nil {
		t.Fatalf("delete disk: %v", err)
	}
	if _, ok := st.Get("sess2", "abc-uuid.pdf"); ok {
		t.Fatal("expected row removed for sess2")
	}
	if _, ok := st.Get("sess1", "abc-uuid.pdf"); !ok {
		t.Fatal("sess1 row should remain")
	}

	// DeleteSession removes the whole session bucket.
	if n, err := st.DeleteSession("sess1"); err != nil || n != 1 {
		t.Fatalf("delete session: n=%d err=%v", n, err)
	}
	if _, ok := st.Get("sess1", "abc-uuid.pdf"); ok {
		t.Fatal("expected sess1 metadata gone after DeleteSession")
	}
}

func TestUploadsMetaReopenPersists(t *testing.T) {
	magicHome := t.TempDir()
	st, err := openUploadsMeta(magicHome)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Upsert("sessX", "f-uuid.docx", "方案.docx", 99, "application/zip"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopening the same database must see the row (persistence across restart).
	st2, err := openUploadsMeta(magicHome)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	row, ok := st2.Get("sessX", "f-uuid.docx")
	if !ok || row.OrigName != "方案.docx" {
		t.Fatalf("expected persisted row, got ok=%v row=%+v", ok, row)
	}
}
