// Package pddsite centralizes supported Pinduoduo mobile sites and URL construction.
package pddsite

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

type Site string

const (
	Pinduoduo Site = "pinduoduo"
	Yangkeduo Site = "yangkeduo"
	Default        = Pinduoduo
)

func Parse(raw string) (Site, error) {
	switch Site(strings.ToLower(strings.TrimSpace(raw))) {
	case "", Pinduoduo:
		return Pinduoduo, nil
	case Yangkeduo:
		return Yangkeduo, nil
	default:
		return "", errors.New("不支持的拼多多站点")
	}
}

func (s Site) Host() string {
	if s == Yangkeduo {
		return "mobile.yangkeduo.com"
	}
	return "mobile.pinduoduo.com"
}

func (s Site) BaseURL() string { return "https://" + s.Host() }

func (s Site) URL(path string, query url.Values) string {
	u := &url.URL{Scheme: "https", Host: s.Host(), Path: path, RawQuery: query.Encode()}
	return u.String()
}

func (s Site) CookieDomain() string { return "." + strings.TrimPrefix(s.Host(), "mobile.") }

// ProfileDir provides a deterministic, site-isolated browser profile location.
func (s Site) ProfileDir(root, accountID string) string {
	accountID = strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(strings.TrimSpace(accountID))
	return filepath.Join(root, "pdd_"+accountID, string(s))
}

func Detect(rawURL string) Site {
	u, err := url.Parse(rawURL)
	if err == nil && strings.EqualFold(u.Hostname(), Yangkeduo.Host()) {
		return Yangkeduo
	}
	return Pinduoduo
}
