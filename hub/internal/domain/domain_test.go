package domain

import (
	"testing"

	"github.com/google/uuid"
)

func dirs() (code, web, docs, design Dir, all []Dir) {
	code = Dir{ID: uuid.New(), Name: "payments-api", Kind: "code"}
	web = Dir{ID: uuid.New(), Name: "web", Kind: "code"}
	docs = Dir{ID: uuid.New(), Name: "docs", Kind: "docs"}
	design = Dir{ID: uuid.New(), Name: "design", Kind: "design"}
	return code, web, docs, design, []Dir{web, code, docs, design}
}

func TestClassifyDir(t *testing.T) {
	for p, want := range map[string]string{
		"/home/a/payments": "code", "/home/a/docs": "docs", "/x/product-wiki/": "docs",
		"/x/Design-System": "design", "C:\\repos\\brand-assets": "design", "/srv/api": "code",
	} {
		if got := ClassifyDir(p); got != want {
			t.Errorf("ClassifyDir(%q) = %s, want %s", p, got, want)
		}
	}
}

func TestSuggestFollowsSkillTable(t *testing.T) {
	code, _, docs, design, all := dirs()
	dev := Suggest("developer", all, "Payments API", nil)
	if len(dev) != 1 || dev[0].DirectoryID != code.ID || dev[0].Mode != ReadWrite || dev[0].Reason == "" {
		t.Errorf("developer: %+v", dev)
	}
	rev := Suggest("reviewer", all, "Payments API", nil)
	if len(rev) != 1 || rev[0].DirectoryID != code.ID || rev[0].Mode != Read {
		t.Errorf("reviewer: %+v", rev)
	}
	des := Suggest("designer", all, "x", nil)
	if len(des) != 2 || des[0].DirectoryID != docs.ID && des[0].DirectoryID != design.ID {
		t.Errorf("designer: %+v", des)
	}
	if w := Suggest("writer", all, "x", nil); len(w) != 1 || w[0].DirectoryID != docs.ID {
		t.Errorf("writer: %+v", w)
	}
	res := Suggest("researcher", all, "x", nil)
	if len(res) != 3 {
		t.Errorf("researcher: %+v", res)
	}
	for _, g := range res {
		if g.Mode != Read {
			t.Errorf("researcher should be read only: %+v", g)
		}
	}
	// No name match: first code directory.
	if d := Suggest("developer", all, "Unrelated", nil); len(d) != 1 || d[0].DirectoryID != all[0].ID {
		t.Errorf("fallback to first code dir: %+v", d)
	}
}

func TestSuggestionsAreClampedToManager(t *testing.T) {
	code, web, docs, _, all := dirs()
	manager := []Grant{{DirectoryID: code.ID, Mode: Read}, {DirectoryID: docs.ID, Mode: ReadWrite}}
	got := Suggest("researcher", all, "Payments API", manager)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	for _, g := range got {
		if g.DirectoryID == web.ID {
			t.Error("suggested a directory the manager can't reach")
		}
	}
	dev := Suggest("developer", all, "Payments API", manager)
	if len(dev) != 1 || dev[0].Mode != Read {
		t.Errorf("developer clamped to manager's read: %+v", dev)
	}
}

func TestWithinLimitAndClamp(t *testing.T) {
	code, web, _, _, _ := dirs()
	manager := []Grant{{DirectoryID: code.ID, Mode: Read}}
	if _, ok := WithinLimit([]Grant{{DirectoryID: code.ID, Mode: Read}}, manager); !ok {
		t.Error("equal grant rejected")
	}
	if _, ok := WithinLimit([]Grant{{DirectoryID: code.ID, Mode: ReadWrite}}, manager); ok {
		t.Error("wider mode accepted")
	}
	if _, ok := WithinLimit([]Grant{{DirectoryID: web.ID, Mode: Read}}, manager); ok {
		t.Error("directory outside manager accepted")
	}
	pruned := Clamp([]Grant{{DirectoryID: code.ID, Mode: ReadWrite}, {DirectoryID: web.ID, Mode: Read}}, manager)
	if len(pruned) != 1 || pruned[0].Mode != Read {
		t.Errorf("clamp: %+v", pruned)
	}
}
