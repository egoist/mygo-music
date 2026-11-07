package lyrics

import (
	"testing"
	"time"

	"mygo-music/internal/music"
)

func TestParseLRC(t *testing.T) {
	lines := Build("[00:01.000] 作词 : 某人\n[00:30.54]故事的小黄花\n[00:34.165][01:10.1]从出生那年就飘着\n[00:40.00]\n",
		"[00:30.54]The little yellow flower\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines: %+v", len(lines), lines)
	}
	if lines[1].At != 30540*time.Millisecond || lines[1].Translation != "The little yellow flower" {
		t.Errorf("line 1: %+v", lines[1])
	}
	if lines[3].At != 70100*time.Millisecond || lines[3].Text != "从出生那年就飘着" {
		t.Errorf("line 3: %+v", lines[3])
	}
	if i := Index(lines, 33*time.Second); i != 1 {
		t.Errorf("index at 33s = %d", i)
	}
	if i := Index(lines, 0); i != -1 {
		t.Errorf("index at 0 = %d", i)
	}
}

func TestSpreadWords(t *testing.T) {
	w := SpreadWords("我爱 you all", 0, 10*time.Second)
	got := []string{}
	for _, x := range w {
		got = append(got, x.Text)
	}
	if len(w) != 4 || got[0] != "我" || got[1] != "爱" || got[2] != "you " || got[3] != "all" {
		t.Fatalf("words %q", got)
	}
	if w[0].Start != 0 || w[3].End > 10*time.Second || w[1].Start != w[0].End {
		t.Errorf("times %+v", w)
	}
}

// A source's own timed lines keep the times of their words, and a line given
// without words or an end still gets both.
func TestFromSourceLines(t *testing.T) {
	lines := FromSource(&music.Lyrics{Lines: []music.LyricLine{
		{Start: 13010, End: 17790, Translation: "Never", Words: []music.LyricWord{
			{Start: 13010, End: 13670, Text: "Hold "},
			{Start: 13670, End: 14340, Text: "on"},
		}},
		{Start: 20000, Text: "Plain line"},
		{Start: 30000, Text: "   "},
	}})
	if len(lines) != 2 {
		t.Fatalf("lines %+v", lines)
	}
	l := lines[0]
	if l.Text != "Hold on" || l.At != 13010*time.Millisecond || l.End != 17790*time.Millisecond || l.Translation != "Never" {
		t.Errorf("line %+v", l)
	}
	if len(l.Words) != 2 || l.Words[1].Start != 13670*time.Millisecond {
		t.Errorf("words %+v", l.Words)
	}
	if lines[1].End != 26*time.Second || len(lines[1].Words) != 2 {
		t.Errorf("plain line %+v", lines[1])
	}
}

// With no lines of its own, a source's LRC is used.
func TestFromSourceLRC(t *testing.T) {
	lines := FromSource(&music.Lyrics{LRC: "[00:01.00]One", Translation: "[00:01.00]Uno"})
	if len(lines) != 1 || lines[0].Translation != "Uno" {
		t.Errorf("lines %+v", lines)
	}
}

func TestQuiets(t *testing.T) {
	s := time.Second
	lines := []Line{
		{At: 0, End: 2 * s, Text: "作词 : 林夕"},
		{At: 2 * s, End: 4 * s, Text: "作曲：陈小霞"},
		{At: 20 * s, End: 25 * s, Text: "一"},
		{At: 26 * s, End: 30 * s, Text: "二"},
		{At: 45 * s, End: 50 * s, Text: "三"},
	}
	got := Quiets(lines, 60*s, 8*s)
	want := []Quiet{{0, 20 * s}, {30 * s, 45 * s}, {50 * s, 60 * s}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("quiet %d: %v, want %v", i, got[i], want[i])
		}
	}
	if q := Quiets(nil, 60*s, 8*s); len(q) != 0 {
		t.Errorf("a song without lyrics: %v", q)
	}
}

func TestIsCredit(t *testing.T) {
	for text, want := range map[string]bool{
		"作词 : 林夕":            true,
		"作曲：陈小霞":             true,
		"编曲: 藤田淳平(Elements)": true,
		"Producer: 周杰伦":      true,
		"我说：你好":              false,
		"时间：是一把刀":            false,
		"悴んだ心 ふるえる眼差し":       false,
		"词：方文山":              true,
		"曲终人散：空余叹":           false,
	} {
		if IsCredit(text) != want {
			t.Errorf("%q: %v", text, !want)
		}
	}
}

func TestLang(t *testing.T) {
	for _, c := range []struct {
		lines []Line
		want  string
	}{
		{[]Line{{Text: "春日影"}, {Text: "悴んだ心 ふるえる眼差し"}}, "ja"},
		{[]Line{{Text: "カタカナだけ"}}, "ja"},
		{[]Line{{Text: "春日影"}, {Text: "我想要的只是"}}, ""},
		{[]Line{{Text: "Hello ・ world ー"}}, ""},
		{nil, ""},
	} {
		if got := Lang(c.lines); got != c.want {
			t.Errorf("Lang(%v) = %q, want %q", c.lines, got, c.want)
		}
	}
}
