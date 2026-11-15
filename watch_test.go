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

// a burst of saves is one reload, once they settle
func TestWatch(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "app.mn"), "1")
	reloadch := make(chan bool, 10)
	go watch(dir, []string{"mn"}, 20*time.Millisecond, reloadch)
	time.Sleep(50 * time.Millisecond)

	// an ignored file doesn't reload
	write(t, filepath.Join(dir, "notes.txt"), "x")
	select {
	case <-reloadch:
		t.Fatal("reloaded for an unwatched extension")
	case <-time.After(100 * time.Millisecond):
	}

	// atomic save: write a temp file and rename it over the original
	for i := range 3 {
		tmp := filepath.Join(dir, "app.mn.tmp")
		write(t, tmp, "saved"+string(rune('a'+i)))
		if err := os.Rename(tmp, filepath.Join(dir, "app.mn")); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-reloadch:
	case <-time.After(time.Second):
		t.Fatal("no reload after a change")
	}
	select {
	case <-reloadch:
		t.Fatal("a burst of saves reloaded more than once")
	case <-time.After(150 * time.Millisecond):
	}
}
