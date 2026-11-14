package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Choosing a style for Now Playing in the settings shows the song playing
// in it, and choosing Cover goes back to the cover.
func TestChoosePlayerStyle(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.settingsView, 780, 800)
	settled(tt)
	shown := "Cover"
	for _, c := range []struct{ item, want string }{{"Vinyl", "vinyl"}, {"Cassette", "cassette"}, {"Cover", ""}} {
		// The select, by the style it shows: its label is the row's too.
		if err := tt.Click(shown); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if err := tt.Click(c.item); err != nil {
			t.Fatalf("%v; showing %q", err, tt.Texts())
		}
		tt.Frame()
		shown = c.item
		if a.settings.PlayerStyle != c.want {
			t.Errorf("chose %s: style %q, want %q", c.item, a.settings.PlayerStyle, c.want)
		}
	}
}

// Flung to the left, a retro player flies off for the next song, as the
// cover does.
func TestFlingRetroPlayer(t *testing.T) {
	for _, style := range playerStyles[1:] {
		a, tt, r := coverApp(t)
		a.settings.PlayerStyle = style.id
		settled(tt)
		drag(tt, r, -r.W*0.8, 0)
		if id := a.pl.Current().ID; id != "3" {
			t.Errorf("%s flung left: playing %s", style.id, id)
		}
	}
}
