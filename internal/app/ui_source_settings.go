package app

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"
	"mygo-music/internal/music"
)

// settingEdit belongs to one source/key. Only the main thread changes it;
// pending writes disable the control until their posted completion arrives.
type settingEdit struct {
	source string
	value  string
	saving bool
	failed bool
}

// typedSourceSetting reports whether the metadata can safely use a native
// control. Unsupported metadata or an unrecognized boolean keeps the existing
// text editor instead of silently normalizing a saved value.
func (a *app) typedSourceSetting(c *ui.Context, lib *library, st music.Setting, p palette) bool {
	source := lib.src.Setting(st.Key)
	switch st.Type {
	case "password":
	case "boolean":
		if source != "" && source != "true" && source != "false" {
			return false
		}
	case "select":
		if !validSettingOptions(st.Options) {
			return false
		}
	default:
		return false
	}
	if a.settingEdits == nil {
		a.settingEdits = map[string]*settingEdit{}
	}
	key := lib.id + "\x00" + st.Key
	draft := a.settingEdits[key]
	if draft == nil || !draft.saving && !draft.failed && draft.value == draft.source && source != draft.source {
		draft = &settingEdit{source: source, value: source}
		a.settingEdits[key] = draft
	}
	ui.Column(c).Gap(4).Children(func() {
		changed := false
		switch st.Type {
		case "boolean":
			on := draft.value == "true"
			ui.Row(c).Gap(10).Children(func() {
				if ui.Switch(c.Key("value"), &on).Label(st.Label).Disabled(draft.saving).Changed() {
					draft.value = strconv.FormatBool(on)
					changed = true
				}
				// Label is accessibility metadata, not the switch's visible text.
				ui.Text(c, st.Label).FontSize(12.5).FontWeight(500)
			})
		case "select":
			ui.Text(c, st.Label).FontSize(12.5).FontWeight(500)
			names := make([]string, 0, len(st.Options)+1)
			chosen := ""
			found := false
			for _, option := range st.Options {
				names = append(names, option.Label)
				if option.Value == draft.value {
					chosen, found = option.Label, true
				}
			}
			if !found {
				// Keep an unlisted legacy value visible and untouched until the
				// listener deliberately chooses one of the declared options.
				chosen = fmt.Sprintf("%s [%s]", T("Current value"), draft.value)
				for slices.Contains(names, chosen) {
					chosen += " "
				}
				names = append([]string{chosen}, names...)
			}
			if ui.Select(c.Key("value"), &chosen, names).Label(st.Label).Disabled(draft.saving).Changed() {
				for _, option := range st.Options {
					if chosen == option.Label {
						draft.value = option.Value
						changed = true
						break
					}
				}
			}
		case "password":
			ui.Text(c, st.Label).FontSize(12.5).FontWeight(500)
			ui.TextInput(c.Key("value"), &draft.value).Label(st.Label).FontSize(13).Password().Disabled(draft.saving)
		}
		if changed {
			a.keepTypedSetting(lib, st.Key, draft)
		}
		if st.Type == "password" || draft.failed {
			ui.Row(c).Gap(8).Children(func() {
				dirty := draft.failed || draft.value != draft.source
				if ui.Button(c.Key("save"), T("Save changes")).Disabled(draft.saving || !dirty).Clicked() {
					a.keepTypedSetting(lib, st.Key, draft)
				}
				if ui.Button(c.Key("discard"), T("Discard changes")).Disabled(draft.saving || !dirty).Clicked() {
					if draft.failed {
						// A source can change memory before persistence fails. Restore
						// the previous value through SetSetting, not just the editor.
						draft.value = draft.source
						a.keepTypedSetting(lib, st.Key, draft)
					} else {
						delete(a.settingEdits, key)
					}
				}
			})
		}
		if st.Help != "" {
			ui.Text(c, st.Help).FontSize(11.5).TextColor(p.muted).LineHeight(1.4)
		}
		if st.Link != "" {
			ui.RichText(c).FontSize(11.5).Children(func() { ui.Link(c, st.Link, st.Link) })
		}
	})
	return true
}

// Ambiguous or incomplete selects fall back to text, never coercing a value.
func validSettingOptions(options []music.SettingOption) bool {
	if len(options) == 0 {
		return false
	}
	values, labels := map[string]bool{}, map[string]bool{}
	for _, option := range options {
		if option.Label == "" || values[option.Value] || labels[option.Label] {
			return false
		}
		values[option.Value], labels[option.Label] = true, true
	}
	return true
}

func (a *app) keepTypedSetting(lib *library, key string, draft *settingEdit) {
	if draft.saving || !draft.failed && draft.value == draft.source {
		return
	}
	draft.saving = true
	value := draft.value
	a.saveSetting(lib, key, value, func(err error) {
		draft.saving, draft.failed = false, err != nil
		if err == nil {
			draft.source = value
		}
	})
}
