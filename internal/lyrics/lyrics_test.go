package lyrics

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestParseLRC(t *testing.T) {
	l := ParseLRC("[ar:Someone]\n[00:12.50]Hello\n[01:02.3][01:30.00]Chorus\n[00:05]Intro\nno stamp")
	if len(l) != 4 || l[0].Text != "Intro" || l[1].At != 12.5 || l[2].At != 62.3 || l[3].Text != "Chorus" {
		t.Fatalf("%+v", l)
	}
}

func TestClean(t *testing.T) {
	if got := CleanTitle("Song (feat. X) [Remastered 2011]"); got != "Song" {
		t.Fatal(got)
	}
	if got := CleanTitle("Song - Live"); got != "Song" {
		t.Fatal(got)
	}
	if got := MainArtist("A, B feat. C"); got != "A" {
		t.Fatal(got)
	}
	if got := MainArtist("Artist feat. Other"); got != "Artist" {
		t.Fatal(got)
	}
}

// Real network: VKTUI_NET_TEST=1 go test ./internal/lyrics -run Net -v
func TestNetProviders(t *testing.T) {
	if os.Getenv("VKTUI_NET_TEST") == "" {
		t.Skip("network")
	}
	q := Query{Artist: "Кино", Title: "Группа крови", Duration: 286}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	for _, p := range []Provider{LRCLIB{}, Genius{}} {
		l, err := p.Get(ctx, q)
		if err != nil {
			t.Errorf("%s: %v", p.Name(), err)
			continue
		}
		first := ""
		if len(l.Synced) > 0 {
			first = l.Synced[0].Text
		} else if len(l.Plain) > 0 {
			first = l.Plain[0]
		}
		t.Logf("%s: synced=%d plain=%d first=%q", p.Name(), len(l.Synced), len(l.Plain), first)
	}
	f := &Finder{Providers: []Provider{LRCLIB{}, Genius{}}, CacheDir: t.TempDir()}
	l, err := f.Find(ctx, q)
	if err != nil || !l.IsSynced() {
		t.Fatalf("finder: %v %+v", err, l)
	}
	t.Logf("finder chose %s", l.Source)
}
