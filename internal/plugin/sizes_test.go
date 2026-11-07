package plugin

import (
	"strings"
	"testing"
)

// A plugin's imageSizes ask the hosts they name for a picture the size it
// shows, over what its URL asks already, and leave pictures of other hosts
// as they are: the app fetched a plugin's covers thousands of pixels across
// for rows of 40, and decoding a screen of them took hundreds of megabytes.
func TestImageSizesAskTheirHosts(t *testing.T) {
	p := load(t, `{"id":"x","imageSizes":[
    {"hosts":["*.img.example.com"],"query":{"param":"{size}y{size}"}},
    {"hosts":["{setting:server}"],"query":{"size":"{size}"}}
  ]}`, `export function home() { return {}; }`)
	if err := p.SetSetting("server", "https://NAS.local:4533"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ in, want string }{
		{"https://p1.img.example.com/a==/1.jpg", "https://p1.img.example.com/a==/1.jpg?param=96y96"},
		{"https://p1.img.example.com/a==/1.jpg?param=1200y1200", "https://p1.img.example.com/a==/1.jpg?param=96y96"},
		{"https://nas.local:4533/rest/getCoverArt?id=al-1&t=tok&s=salt", "https://nas.local:4533/rest/getCoverArt?id=al-1&t=tok&s=salt&size=96"},
		{"https://other.example.com/cover.jpg", ""},
		{"file:///music/cover.jpg", ""},
	} {
		got, ok := p.SizedImage(mustURL(c.in), 96)
		switch {
		case c.want == "" && ok:
			t.Errorf("%s: sized to %s, want it as it is", c.in, got)
		case c.want != "" && (!ok || got.String() != c.want):
			t.Errorf("%s: %v, want %s", c.in, got, c.want)
		}
	}
}

// imageSizes for every host, or for none, keep the plugin from loading.
func TestImageSizesNameHosts(t *testing.T) {
	for _, manifest := range []string{
		`{"id":"x","imageSizes":[{"hosts":["*"],"query":{"w":"{size}"}}]}`,
		`{"id":"x","imageSizes":[{"hosts":[],"query":{"w":"{size}"}}]}`,
		`{"id":"x","imageSizes":[{"hosts":["img.example.com"]}]}`,
	} {
		dir, data := write(t, manifest, `export function home() { return {}; }`)
		if _, err := Load(dir, data); err == nil || !strings.Contains(err.Error(), "imageSizes") {
			t.Errorf("%s loaded: %v", manifest, err)
		}
	}
}
