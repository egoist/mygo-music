package mp4

import "testing"

// AAC-LC plays, and HE-AAC, which the decoder refuses, doesn't: it isn't
// listed, to fail once it's played.
func TestPlayable(t *testing.T) {
	for _, tt := range []struct {
		name  string
		track Track
		want  bool
	}{
		{"ALAC", Track{Codec: "alac"}, true},
		{"AAC-LC", Track{Codec: "mp4a", Config: []byte{0x12, 0x10}}, true},
		{"HE-AAC", Track{Codec: "mp4a", Config: []byte{0x2b, 0x12, 0x08}}, false},
		{"other", Track{Codec: "Opus"}, false},
	} {
		if got := tt.track.Playable(); got != tt.want {
			t.Errorf("%s: playable %v", tt.name, got)
		}
	}
}
