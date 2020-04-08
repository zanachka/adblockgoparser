package adblockgoparser

import (
	"path/filepath"
	"strings"
)

type Matcher struct {
	addressPartMatcher *PathMatcher
	domainNameRules    []*ruleAdBlock
	exactAddressRules  []*ruleAdBlock
}

type PathMatcher struct {
	next  map[rune]*PathMatcher
	rules []*ruleAdBlock
}

func (matcher *Matcher) add(rule *ruleAdBlock) {
	var runes []rune
	switch rule.ruleType {
	case addressPart:
		runes = []rune(rule.ruleText)
		matcher.addressPartMatcher.addPath(runes, rule)
	case domainName:
		matcher.domainNameRules = append(matcher.domainNameRules, rule)
	case exactAddress:
		matcher.exactAddressRules = append(matcher.exactAddressRules, rule)
	}
}

func (pathMatcher *PathMatcher) addPath(runes []rune, rule *ruleAdBlock) {
	if len(runes) == 0 {
		pathMatcher.rules = append(pathMatcher.rules, rule)
		return
	}

	if string(runes[0]) == "^" {
		pathMatcher.rules = append(pathMatcher.rules, rule)
		return
	}

	if _, ok := pathMatcher.next[runes[0]]; !ok {
		pathMatcher.next[runes[0]] = &PathMatcher{
			next: map[rune]*PathMatcher{},
		}
	}

	pathMatcher.next[runes[0]].addPath(runes[1:], rule)
}

func (matcher *Matcher) Match(req *Request) bool {
	path := req.URL.Path
	pathRunes := []rune(path)

	// Match path
	for i := range pathRunes {
		match := matcher.addressPartMatcher.findNext(pathRunes[i:], req)
		if match {
			return true
		}
	}

	// Match domain and subdomains
	hostname := strings.ToLower(req.URL.Hostname())
	for _, rule := range matcher.domainNameRules {
		allowed := true
		if len(rule.domains) > 0 {
			for domain, active := range rule.domains {
				if !(strings.Contains(hostname, domain) == active) {
					allowed = false
					break
				}
			}
			if allowed && strings.HasSuffix(hostname, rule.ruleText[2:len(rule.ruleText)-1]) {
				matchDomain := true
				matchOption := true
				if len(rule.options) > 0 {
					matchOption = false
					for option, active := range rule.options {
						switch {
						case option == "xmlhttprequest":
						case option == "third-party":
						case option == "script":
							switch filepath.Ext(path) {
							case ".js":
								matchOption = active
							default:
								matchOption = !active
							}
						case option == "image":
							switch filepath.Ext(path) {
							case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".tiff", ".psd", ".raw", ".bmp", ".heif", ".indd", ".jpeg2000":
								matchOption = active
							default:
								matchOption = !active
							}
						case option == "stylesheet":
							switch filepath.Ext(path) {
							case ".css":
								matchOption = active
							default:
								matchOption = !active
							}
						case option == "font":
							switch filepath.Ext(path) {
							case ".otf", ".ttf", ".fnt":
								matchOption = active
							default:
								matchOption = !active
							}
						}
					}
				}
				if matchDomain && matchOption {
					return true
				}
			}
		}
	}

	// Match exact address
	uri := strings.ToLower(req.URL.String())
	for _, rule := range matcher.exactAddressRules {
		if uri == rule.ruleText[1:len(rule.ruleText)-1] {
			return true
		}
	}

	return false
}

func (pathMatcher *PathMatcher) findNext(runes []rune, req *Request) bool {
	match := false
	if len(pathMatcher.rules) != 0 {
		path := strings.ToLower(req.URL.Path)
		if strings.HasSuffix(path, ".gz") {
			path = path[:len(path)-len(".gz")]
		}
		for _, rule := range pathMatcher.rules {
			allowed := true
			if len(rule.domains) > 0 {
				hostname := strings.ToLower(req.URL.Hostname())
				for domain, active := range rule.domains {
					if !(strings.Contains(hostname, domain) == active) {
						allowed = false
						break
					}
				}
			}

			if allowed && rule.regex.MatchString(req.URL.String()) { // This line need to be removed and add simpler validation
				match = true
				matchOption := true
				if len(rule.options) > 0 {
					matchOption = false
					for option, active := range rule.options {
						switch {
						case option == "xmlhttprequest":
						case option == "third-party":
						case option == "script":
							switch filepath.Ext(path) {
							case ".js":
								matchOption = matchOption || active
							default:
								matchOption = matchOption || !active
							}
						case option == "image":
							switch filepath.Ext(path) {
							case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".tiff", ".psd", ".raw", ".bmp", ".heif", ".indd", ".jpeg2000":
								matchOption = matchOption || active
							default:
								matchOption = matchOption || !active
							}
						case option == "stylesheet":
							switch filepath.Ext(path) {
							case ".css":
								matchOption = matchOption || active
							default:
								matchOption = matchOption || !active
							}
						case option == "font":
							switch filepath.Ext(path) {
							case ".otf", ".ttf", ".fnt":
								matchOption = matchOption || active
							default:
								matchOption = matchOption || !active
							}
						}
					}
				}
				if match && matchOption {
					return true
				}
			}
		}
	}
	if len(runes) == 0 {
		return false
	}

	if _, ok := pathMatcher.next[runes[0]]; ok {
		match = pathMatcher.next[runes[0]].findNext(runes[1:], req)
		if match {
			return true
		}
	}

	if _, ok := pathMatcher.next['*']; ok && !match {
		for i := range runes {
			match := pathMatcher.next['*'].findNext(runes[i:], req)
			if match {
				return true
			}
		}
	}

	return false
}
