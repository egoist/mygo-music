package app

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"mygo-music/internal/music"
)

// An animated WebP cover, as some playlists have, shows its first frame,
// in the bitmap too, which decodes its data again once it drops its pixels.
func TestAnimatedWebPCover(t *testing.T) {
	path, err := filepath.Abs("testdata/animated.webp")
	if err != nil {
		t.Fatal(err)
	}
	bmp, img, _, err := loadArt(music.FileURL(path))
	if err != nil {
		t.Fatal(err)
	}
	if r := img.Bounds(); r.Dx() != 24 || r.Dy() != 24 {
		t.Errorf("first frame is %v, want 24×24", r)
	}
	if w, h := bmp.Size(); w != 24 || h != 24 {
		t.Errorf("bitmap is %d×%d, want 24×24", w, h)
	}
}

// A cover much bigger than it shows makes its bitmap of the smaller copy
// alone. It made one of the whole cover first, then dropped it: a 96-pixel
// thumbnail of a 4000-pixel cover took some 120 MB to load.
func TestBigCoverThumbnailSkipsFullBitmap(t *testing.T) {
	const n = 2000
	src := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			src.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	path := filepath.Join(t.TempDir(), "cover.jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, src, nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	src = nil
	key := sized(music.FileURL(path), 96)

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	bmp, img, _, err := loadArt(key)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := bmp.Size(); w != 96 || h != 96 {
		t.Errorf("bitmap is %d×%d, want 96×96", w, h)
	}
	if r := img.Bounds(); r.Dx() != 96 || r.Dy() != 96 {
		t.Errorf("image is %v, want 96×96", r)
	}
	// Decoding the JPEG takes some 1.5 bytes a pixel and scaling it some
	// 1.5 more; a full RGBA bitmap took 4, and decoding it again 1.5.
	if got, budget := after.TotalAlloc-before.TotalAlloc, uint64(4*n*n); got > budget {
		t.Errorf("loading a 96-pixel thumbnail of a %d-pixel cover allocated %d bytes, budget %d", n, got, budget)
	}
}

// Once the covers take more than the limit, those asked for least
// recently go, so that a long session does not keep every cover it
// showed; one asked for again stays, however long ago it loaded.
func TestArtCacheDropsLeastRecentCovers(t *testing.T) {
	now := time.Unix(1e9, 0)
	c := newArtCache(func(func()) {})
	c.now, c.limit = func() time.Time { return now }, 100
	for _, key := range []string{"first", "second", "third"} {
		c.add(key, &artwork{used: now, size: 30})
		now = now.Add(time.Minute)
	}
	c.get("first", false)
	c.add("fourth", &artwork{used: now, size: 30})
	if _, ok := c.art["second"]; ok {
		t.Error("kept the cover asked for least recently over the limit")
	}
	for _, key := range []string{"first", "fourth"} {
		if _, ok := c.art[key]; !ok {
			t.Errorf("dropped %q, asked for last", key)
		}
	}
}

// Covers stay under the limit however long since they showed. They went
// after 5 minutes not asked for: going back to a page after a while on
// others, or to the window after a while away, loaded them again.
func TestArtCacheKeepsCoversUnderLimit(t *testing.T) {
	now := time.Unix(1e9, 0)
	c := newArtCache(func(func()) {})
	c.now = func() time.Time { return now }
	page := &artwork{used: now, size: 100 << 10}
	c.add("page", page)
	for range 60 {
		now = now.Add(time.Minute)
		c.get("other", false)
	}
	if c.get("page", false) != page {
		t.Error("dropped a cover not asked for in an hour, under the limit")
	}
}

// A cover dropped while something still holds it comes back as it was.
// It loaded again: Now Playing's cover went while other pages showed,
// though Now Playing still held it, and flashed in once it opened again.
func TestArtCacheKeepsHeldCovers(t *testing.T) {
	now := time.Unix(1e9, 0)
	c := newArtCache(func(func()) {})
	c.now, c.limit = func() time.Time { return now }, 60
	held := &artwork{used: now, size: 30}
	c.add("now playing", held)
	for _, key := range []string{"other", "another"} {
		now = now.Add(time.Minute)
		c.add(key, &artwork{used: now, size: 30})
	}
	if _, ok := c.art["now playing"]; ok {
		t.Fatal("kept a cover over the limit")
	}
	if got := c.get("now playing", false); got != held {
		t.Error("loaded again a cover something still held")
	}
	runtime.KeepAlive(held)
}
