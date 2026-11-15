package run

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseExts(t *testing.T) {
	got := ParseExts(" mn, .rest,,css ")
	if !slices.Equal(got, []string{"mn", "rest", "css"}) {
		t.Errorf("got %v", got)
	}
	if got := ParseExts(""); len(got) != 0 {
		t.Errorf("empty: got %v", got)
	}
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"site.rest", "src/app.mn", "src/notes.txt", "src/app.mn~",
		".git/HEAD.mn", "node_modules/x/y.mn", ".hidden.mn",
	} {
		write(t, filepath.Join(dir, p), "x")
	}
	keys := func(m map[string]fileStat) []string {
		ks := []string{}
		for k := range m {
			rel, _ := filepath.Rel(dir, k)
			ks = append(ks, rel)
		}
		slices.Sort(ks)
		return ks
	}
	if got := keys(scan(dir, []string{"mn", "rest"})); !slices.Equal(got, []string{"site.rest", "src/app.mn"}) {
		t.Errorf("mn,rest: got %v", got)
	}
	if got := keys(scan(dir, []string{"*"})); !slices.Equal(got, []string{"site.rest", "src/app.mn", "src/notes.txt"}) {
		t.Errorf("*: got %v", got)
	}
}

func TestChanged(t *testing.T) {
	now := time.Now()
	a := map[string]fileStat{"a": {now, 1}, "b": {now, 1}}
	cases := []struct {
		name string
		b    map[string]fileStat
		want bool
	}{
		{"same", map[string]fileStat{"a": {now, 1}, "b": {now, 1}}, false},
		{"edited", map[string]fileStat{"a": {now.Add(time.Second), 1}, "b": {now, 1}}, true},
		{"resized", map[string]fileStat{"a": {now, 2}, "b": {now, 1}}, true},
		{"added", map[string]fileStat{"a": {now, 1}, "b": {now, 1}, "c": {now, 1}}, true},
		{"removed", map[string]fileStat{"a": {now, 1}}, true},
	}
	for _, c := range cases {
		if _, got := changed(a, c.b); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

// a burst of saves is one batch, once they settle, with what's different from
// before it
func TestWatch(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "app.mn"), "1")
	write(t, filepath.Join(dir, "old.mn"), "1")
	changech := make(chan []Change, 10)
	go watch(dir, []string{"mn"}, 20*time.Millisecond, changech)
	time.Sleep(50 * time.Millisecond)

	// an ignored file doesn't count
	write(t, filepath.Join(dir, "notes.txt"), "x")
	select {
	case c := <-changech:
		t.Fatalf("changes for an unwatched extension: %v", c)
	case <-time.After(100 * time.Millisecond):
	}

	// atomic saves (a temp file renamed over the original), a new file and
	// a removed one, all at once
	for i := range 3 {
		tmp := filepath.Join(dir, "app.mn.tmp")
		write(t, tmp, "saved"+string(rune('a'+i)))
		if err := os.Rename(tmp, filepath.Join(dir, "app.mn")); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	write(t, filepath.Join(dir, "src", "new.mn"), "x")
	os.Remove(filepath.Join(dir, "old.mn"))

	var got []Change
	select {
	case got = <-changech:
	case <-time.After(time.Second):
		t.Fatal("no changes")
	}
	want := []Change{
		{"modified", "app.mn"},
		{"removed", "old.mn"},
		{"added", filepath.Join("src", "new.mn")},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	select {
	case c := <-changech:
		t.Fatalf("a burst of saves was more than one batch: %v", c)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestDescribe(t *testing.T) {
	if got := describe([]Change{{"modified", "a.md"}}); got != "a.md modified" {
		t.Error(got)
	}
	if got := describe([]Change{{"modified", "a.md"}, {"added", "b.md"}}); got != "2 files changed" {
		t.Error(got)
	}
}
