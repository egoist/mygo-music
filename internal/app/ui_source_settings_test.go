package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	"mygo-music/internal/music"
)

// These sources model plugins that change memory before a save fails. Reads
// during UI rebuilds must be synchronized just as Plugin.Setting is.
type settingSource struct {
	*fakeSource
	mu sync.Mutex
}

func (s *settingSource) Setting(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vals[key]
}

func (s *settingSource) putSetting(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vals[key] = value
}

func newSettingSource(a *app) *settingSource {
	src := &settingSource{fakeSource: fake(a)}
	a.library.src = src
	return src
}

func settingTester(a *app, st music.Setting) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) { a.sourceSetting(c, a.library, st, paletteFor(c.Theme())) }, 640, 500)
}

func clickSettingRole(t *testing.T, tt *ui.Tester, role ui.Role, label string) {
	t.Helper()
	n, ok := tt.AccessByRole(role, label)
	if !ok {
		t.Fatalf("missing control %q", label)
	}
	r := n.Bounds
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
}

// A switch saves canonical strings once, and ignores more clicks until completion.
func TestTypedSettingBoolean(t *testing.T) {
	a := newTestApp()
	post := posts(a)
	src := fake(a)
	kept := make(chan string, 4)
	release := make(chan struct{})
	defer close(release)
	src.setSetting = func(key, value string) error { kept <- value; <-release; return nil }
	st := music.Setting{Key: "enabled", Type: "boolean", Label: "Enabled"}
	tt := settingTester(a, st)
	tt.Frame()
	select {
	case <-kept:
		t.Fatal("rendering an empty setting must not write false")
	default:
	}
	clickSettingRole(t, tt, ui.RoleSwitch, "Enabled")
	if got := wait(t, kept, "boolean setting"); got != "true" {
		t.Fatalf("saved %q", got)
	}
	tt.Frame()
	if n, ok := tt.AccessByRole(ui.RoleSwitch, "Enabled"); !ok || !n.Disabled {
		t.Fatal("pending switch was not disabled")
	}
	clickSettingRole(t, tt, ui.RoleSwitch, "Enabled")
	select {
	case <-kept:
		t.Fatal("duplicate write while saving")
	default:
	}
	// The source owns persistence. Reflect its completed value before the UI callback.
	src.vals[st.Key] = "true"
	release <- struct{}{}
	runPost(t, post)
	tt.Frame()
	clickSettingRole(t, tt, ui.RoleSwitch, "Enabled")
	if got := wait(t, kept, "disabled boolean setting"); got != "false" {
		t.Fatalf("saved %q", got)
	}
}

// Select labels are presentation only; blank and unknown legacy values stay untouched.
func TestTypedSettingSelect(t *testing.T) {
	for _, initial := range []string{"", "older-mode"} {
		t.Run(fmt.Sprintf("value=%s", initial), func(t *testing.T) {
			a := newTestApp()
			posts(a)
			src := fake(a)
			src.vals["auth"] = initial
			kept := make(chan string, 4)
			src.setSetting = func(key, value string) error { kept <- value; return nil }
			st := music.Setting{Key: "auth", Type: "select", Label: "Sign-in method", Options: []music.SettingOption{{Value: "", Label: "Automatic"}, {Value: "token", Label: "Token"}, {Value: "legacy", Label: "Password"}}}
			tt := settingTester(a, st)
			tt.Frame()
			select {
			case <-kept:
				t.Fatal("opening a select changed the stored value")
			default:
			}
			clickSettingRole(t, tt, ui.RolePopUpButton, st.Label)
			if err := tt.Click("Password"); err != nil {
				t.Fatal(err)
			}
			if got := wait(t, kept, "selected value"); got != "legacy" {
				t.Fatalf("saved label instead of value: %q", got)
			}
		})
	}
}

// Unknown types, malformed choices and noncanonical booleans retain the old text editor.
func TestTypedSettingFallback(t *testing.T) {
	for _, st := range []music.Setting{
		{Key: "key", Label: "Field"},
		{Key: "key", Label: "Field", Type: "text"},
		{Key: "key", Label: "Field", Type: "future-type"},
		{Key: "key", Label: "Field", Type: "boolean"},
		{Key: "key", Label: "Field", Type: "select"},
		{Key: "key", Label: "Field", Type: "select", Options: []music.SettingOption{{Value: "a"}}},
		{Key: "key", Label: "Field", Type: "select", Options: []music.SettingOption{{Value: "a", Label: "Same"}, {Value: "b", Label: "Same"}}},
		{Key: "key", Label: "Field", Type: "select", Options: []music.SettingOption{{Value: "a", Label: "A"}, {Value: "a", Label: "B"}}},
	} {
		t.Run(st.Type+fmt.Sprint(st.Options), func(t *testing.T) {
			a := newTestApp()
			posts(a)
			src := fake(a)
			src.vals["key"] = "legacy-value"
			kept := make(chan string, 4)
			src.setSetting = func(key, value string) error { kept <- value; return nil }
			tt := settingTester(a, st)
			tt.Frame()
			clickSettingRole(t, tt, ui.RoleTextField, st.Label)
			tt.Key(ui.Cmd, ui.KeyA)
			tt.Type("edited")
			tt.Key(0, ui.KeyEnter)
			if got := wait(t, kept, "text setting"); got != "edited" {
				t.Fatalf("text fallback saved %q", got)
			}
		})
	}
}

// Password typing, Enter and blur do not persist partial credentials; Save preserves whitespace.
func TestTypedSettingPassword(t *testing.T) {
	a := newTestApp()
	post := posts(a)
	src := fake(a)
	src.vals["password"] = " old secret "
	kept := make(chan string, 4)
	src.setSetting = func(key, value string) error { kept <- value; return nil }
	st := music.Setting{Key: "password", Type: "password", Label: "Password"}
	tt := settingTester(a, st)
	tt.Frame()
	if n, ok := tt.AccessByRole(ui.RoleTextField, "Password"); !ok || !n.Password || n.Value != "" {
		t.Fatal("password exposed through accessibility")
	}
	clickSettingRole(t, tt, ui.RoleTextField, "Password")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(" new secret ")
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyTab)
	select {
	case <-kept:
		t.Fatal("password saved before explicit Save")
	default:
	}
	clickSettingRole(t, tt, ui.RoleButton, "Save changes")
	if got := wait(t, kept, "saved password"); got != " new secret " {
		t.Fatalf("password modified: %q", got)
	}
	src.vals["password"] = " new secret "
	runPost(t, post)
	tt.Frame()
	clickSettingRole(t, tt, ui.RoleTextField, "Password")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("discard me")
	clickSettingRole(t, tt, ui.RoleButton, "Discard changes")
	tt.Frame()
	if draft := a.settingEdits[a.library.id+"\x00password"]; draft.value != " new secret " {
		t.Fatal("discard did not restore the source value")
	}
	select {
	case <-kept:
		t.Fatal("discard persisted the draft")
	default:
	}
}

// Retrying a failed save must call SetSetting again even if it changed memory before failing.
func TestTypedSettingSaveRetry(t *testing.T) {
	a := newTestApp()
	post := posts(a)
	src := newSettingSource(a)
	kept := make(chan string, 4)
	attempts := 0
	src.setSetting = func(key, value string) error {
		attempts++
		src.putSetting(key, value)
		kept <- value
		if attempts == 1 {
			return errors.New("storage unavailable")
		}
		return nil
	}
	st := music.Setting{Key: "enabled", Type: "boolean", Label: "Enabled"}
	tt := settingTester(a, st)
	tt.Frame()
	clickSettingRole(t, tt, ui.RoleSwitch, st.Label)
	wait(t, kept, "first save")
	runPost(t, post)
	tt.Frame()
	if !strings.Contains(a.toast, "storage unavailable") {
		t.Fatal("save error was not reported")
	}
	clickSettingRole(t, tt, ui.RoleButton, "Save changes")
	wait(t, kept, "retried save")
	runPost(t, post)
	tt.Frame()
	if attempts != 2 || a.settingEdits[a.library.id+"\x00enabled"].failed {
		t.Fatal("retry was incorrectly treated as already persisted")
	}
}

// Discard after a failed write restores the last successful value through the source.
func TestTypedSettingFailedDiscard(t *testing.T) {
	a := newTestApp()
	post := posts(a)
	src := newSettingSource(a)
	src.putSetting("password", "old")
	kept := make(chan string, 4)
	attempts := 0
	src.setSetting = func(key, value string) error {
		attempts++
		src.putSetting(key, value)
		kept <- value
		if attempts == 1 {
			return errors.New("storage unavailable")
		}
		return nil
	}
	st := music.Setting{Key: "password", Type: "password", Label: "Password"}
	tt := settingTester(a, st)
	tt.Frame()
	clickSettingRole(t, tt, ui.RoleTextField, st.Label)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("new")
	clickSettingRole(t, tt, ui.RoleButton, "Save changes")
	wait(t, kept, "failed password save")
	runPost(t, post)
	tt.Frame()
	clickSettingRole(t, tt, ui.RoleButton, "Discard changes")
	if got := wait(t, kept, "restored password"); got != "old" {
		t.Fatalf("discard restored %q instead of the last successful value", got)
	}
	runPost(t, post)
	tt.Frame()
	if src.Setting("password") != "old" || a.settingEdits[a.library.id+"\x00password"].failed {
		t.Fatal("discard lost the pending failure without restoring the setting")
	}
}

// Settings from one source persist in initiation order instead of racing whole-map writes.
func TestTypedSettingSaveOrder(t *testing.T) {
	a := newTestApp()
	post := posts(a)
	kept := make(chan string, 32)
	fake(a).setSetting = func(key, value string) error { kept <- value; return nil }
	for i := range 20 {
		a.saveSetting(a.library, "key", fmt.Sprint(i), func(error) {})
	}
	for i := range 20 {
		if got := wait(t, kept, "ordered save"); got != fmt.Sprint(i) {
			t.Fatalf("save %d persisted out of order: %q", i, got)
		}
		runPost(t, post)
	}
}

// A blurred legacy field queues once and can undo an edit before an earlier save finishes.
func TestTypedSettingLegacyPending(t *testing.T) {
	a := newTestApp()
	post := posts(a)
	src := fake(a)
	src.vals["key"] = "A"
	kept := make(chan string, 8)
	release := make(chan struct{})
	defer close(release)
	src.setSetting = func(key, value string) error {
		kept <- key + ":" + value
		if key == "blocker" {
			<-release
		}
		return nil
	}
	a.saveSetting(a.library, "blocker", "busy", nil)
	wait(t, kept, "blocking save")
	st := music.Setting{Key: "key", Label: "Text setting"}
	tt := settingTester(a, st)
	tt.Frame()
	clickSettingRole(t, tt, ui.RoleTextField, st.Label)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("B")
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyTab)
	pendingKey := a.library.id + "\x00key"
	pending := a.settingPending[pendingKey]
	for range 5 {
		tt.Frame()
	}
	if pending == nil || a.settingPending[pendingKey] != pending {
		t.Fatal("blurred text queued duplicate writes on subsequent frames")
	}
	clickSettingRole(t, tt, ui.RoleTextField, st.Label)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("A")
	tt.Key(0, ui.KeyEnter)
	if a.pendingSetting(a.library, "key") != "A" {
		t.Fatal("returning to the original value lost the latest edit")
	}
	release <- struct{}{}
	for _, want := range []string{"key:B", "key:A"} {
		if got := wait(t, kept, "queued text save"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
	for range 3 {
		runPost(t, post)
	}
}

// Typed controls remain readable at narrow/desktop widths in both themes.
func TestTypedSettingFrames(t *testing.T) {
	dir := os.Getenv("MM_FRAMES")
	if dir == "" {
		t.Skip("MM_FRAMES not set")
	}
	for _, width := range []int{640, 1000} {
		for _, dark := range []bool{false, true} {
			a := newTestApp()
			src := fake(a)
			src.vals["password"] = "not-a-real-password"
			fields := []music.Setting{
				{Key: "url", Label: "Server addresses", Help: "The original text setting is unchanged."},
				{Key: "password", Type: "password", Label: "Password", Help: "Masked here; still stored as plain text."},
				{Key: "auth", Type: "select", Label: "Sign-in method", Options: []music.SettingOption{{Value: "", Label: "Automatic (token)"}, {Value: "legacy", Label: "Legacy password"}}},
				{Key: "enabled", Type: "boolean", Label: "Save listening history", Help: "Send played tracks to your server."},
			}
			tt := ui.NewTester(func(c *ui.Context) {
				p := paletteFor(c.Theme())
				ui.Column(c).Padding(24).Gap(20).Children(func() {
					for _, st := range fields {
						a.sourceSetting(c.Key(st.Key), a.library, st, p)
					}
				})
			}, width, 700)
			tt.SetDark(dark)
			settled(tt)
			saveFrame(t, dir, fmt.Sprintf("setting-types-%d-dark-%v", width, dark), tt.Image())
		}
	}
}
