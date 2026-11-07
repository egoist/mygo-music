package plugin

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ImageSizes, in plugin.json, say how to ask a host the plugin's pictures
// are on for one the size the app shows it at, for a service whose image
// server makes them, rather than have the app fetch the picture as large
// as it was made and shrink it, a few megabytes each:
//
//	"imageSizes": [{
//	  "hosts": ["*.img.example.com"],
//	  "query": {"param": "{size}y{size}"}
//	}]
//
// {size} is how many pixels the app shows it across.
type ImageSize struct {
	// Hosts are those of the pictures it's for: img.example.com,
	// *.example.com, or {setting:key}, the host of the server a setting
	// names. * isn't one: the app shows pictures of other plugins too.
	Hosts []string `json:"hosts"`
	// Query is what the picture's URL asks for, set over what it has.
	Query map[string]string `json:"query"`
}

// checkImageSizes reports what's wrong with m's imageSizes, if anything, as
// it loads.
func checkImageSizes(m *Manifest) error {
	for i, s := range m.ImageSizes {
		if len(s.Hosts) == 0 || len(s.Query) == 0 {
			return fmt.Errorf("imageSizes[%d] needs hosts and query", i)
		}
		for _, h := range s.Hosts {
			if key, ok := strings.CutPrefix(h, "{setting:"); ok {
				if key, ok = strings.CutSuffix(key, "}"); !ok || key == "" || strings.ContainsAny(key, "{}") {
					return fmt.Errorf("imageSizes[%d]: host %q is neither a host nor {setting:key}", i, h)
				}
				continue
			}
			if h == "" || h == "*" || h == "*." || strings.ContainsAny(h, "{}/:") {
				return fmt.Errorf("imageSizes[%d]: host %q isn't a host or *.domain", i, h)
			}
		}
		for k := range s.Query {
			if k == "" {
				return fmt.Errorf("imageSizes[%d]: query has a parameter with no name", i)
			}
		}
	}
	return nil
}

// SizedImage answers the URL to fetch the picture at u with, to show it px
// pixels across, as the plugin's imageSizes ask its host for one, and
// false for a picture of no host they name.
func (p *Plugin) SizedImage(u *url.URL, px int) (*url.URL, bool) {
	if len(p.manifest.ImageSizes) == 0 || u == nil || px <= 0 || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, false
	}
	host := strings.ToLower(u.Hostname())
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.manifest.ImageSizes {
		if !p.sizeReaches(s, host) {
			continue
		}
		set := url.Values{}
		for k, v := range s.Query {
			set.Set(k, strings.ReplaceAll(v, "{size}", strconv.Itoa(px)))
		}
		sized := *u
		sized.RawQuery = withQuery(u.RawQuery, set)
		return &sized, true
	}
	return nil, false
}

// withQuery is the query raw with set's parameters set. A query that has
// none of them already keeps its own as they came, and set's follow: a
// service may sign its pictures' URLs, which a query written over again
// might change.
func withQuery(raw string, set url.Values) string {
	if raw == "" {
		return set.Encode()
	}
	q, err := url.ParseQuery(raw)
	has := false
	for k := range set {
		if _, ok := q[k]; ok {
			has = true
		}
	}
	if err != nil || !has {
		return raw + "&" + set.Encode()
	}
	for k, v := range set {
		q[k] = v
	}
	return q.Encode()
}

// sizeReaches reports whether s is for host. The caller holds p.mu.
func (p *Plugin) sizeReaches(s ImageSize, host string) bool {
	for _, h := range s.Hosts {
		if key, ok := strings.CutPrefix(h, "{setting:"); ok {
			if sh := serverHost(p.settings[strings.TrimSuffix(key, "}")]); sh != "" && sh == host {
				return true
			}
			continue
		}
		h = strings.ToLower(h)
		if h == host || strings.HasPrefix(h, "*.") && strings.HasSuffix(host, h[1:]) {
			return true
		}
	}
	return false
}
