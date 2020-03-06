package adblockgoparser

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/google/logger"
)

var (
	ErrSkipComment = errors.New("Commented rules are skipped")
	ErrSkipHTML    = errors.New("HTML rules are skipped")
	binaryOptions  = []string{
		"document",
		"domain",
		"elemhide",
		"font",
		"genericblock",
		"generichide",
		"image",
		"matchcase",
		"media",
		"object",
		"other",
		"ping",
		"popup",
		"script",
		"stylesheet",
		"subdocument",
		"thirdparty",
		"webrtc",
		"websocket",
		"xmlhttprequest",
	}
	optionsSplitPat = fmt.Sprintf(",(~?(?:%v))", strings.Join(binaryOptions, "|"))
	optionsSplitRe  = regexp.MustCompile(optionsSplitPat)
)

// Structs

type Request struct {
	// parsed full URL of the request
	URL *url.URL
	// a value of Origin header
	Origin string
	// a value of Referer header
	Referer string
	// Defines is request looks like XHLHttpRequest
	IsXHR bool
}

type ruleAdBlock struct {
	raw         string
	ruleText    string
	regexString string
	regex       *regexp.Regexp
	options     map[string]bool
	isException bool
	domains     map[string]bool
}

func (rule *ruleAdBlock) Match(url string) bool {
	if rule.regex == nil {
		rule.regex = regexp.MustCompile(rule.regexString)
	}
	return rule.regex.MatchString(url)
}

func ParseRule(ruleText string) (*ruleAdBlock, error) {
	rule := &ruleAdBlock{
		domains:  map[string]bool{},
		options:  map[string]bool{},
		raw:      ruleText,
		ruleText: strings.TrimSpace(ruleText),
	}

	isComment := strings.Contains(rule.ruleText, "!") || strings.Contains(rule.ruleText, "[Adblock")
	if isComment {
		return nil, ErrSkipComment
	}

	isHTMLRule := strings.Contains(rule.ruleText, "##") || strings.Contains(rule.ruleText, "#@#")
	if isHTMLRule {
		return nil, ErrSkipHTML
	}

	rule.isException = strings.HasPrefix(rule.ruleText, "@@")
	if rule.isException {
		rule.ruleText = rule.ruleText[2:]
	}

	if strings.Contains(rule.ruleText, "$") {
		var option string

		parts := strings.SplitN(rule.ruleText, "$", 2)
		length := len(parts)

		if length > 0 {
			rule.ruleText = parts[0]
		}

		if length > 1 {
			option = parts[1]
		}

		options := strings.Split(option, ",")
		for _, option := range options {
			if strings.HasPrefix(option, "domain=") {
				rule.domains = parseDomainOption(option)
			} else {
				rule.options[strings.TrimPrefix(option, "~")] = !strings.HasPrefix(option, "~")
			}
		}
	}

	rule.regexString = ruleToRegexp(rule.ruleText)

	return rule, nil
}

type RuleSet struct {
	rules       []*ruleAdBlock
	regexString string
	regex       *regexp.Regexp
}

func (ruleSet *RuleSet) Match(url string) bool {
	if ruleSet.regex == nil {
		ruleSet.regex = regexp.MustCompile(ruleSet.regexString)
	}
	return ruleSet.regex.MatchString(url)
}

func (ruleSet *RuleSet) Allow(url string) bool {
	return !ruleSet.Match(url)
}

func NewRuleSet(rules []*ruleAdBlock) *RuleSet {
	r := &RuleSet{
		rules: rules,
	}
	return r
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)

	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	lines := []string{}
	for line := []byte{}; err == nil; line, _, err = reader.ReadLine() {
		sl := strings.TrimSuffix(string(line), "\n\r")
		if len(sl) == 0 {
			continue
		}
		lines = append(lines, sl)
	}

	return lines, nil
}

func NewRulesSetFromFile(path string) (*RuleSet, error) {
	logger.Init("NewRulesSetFromFile", true, true, ioutil.Discard)
	logger.SetFlags(log.LstdFlags)

	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}
	return NewRuleSetFromStr(lines)
}

func NewRuleSetFromStr(rulesStr []string) (*RuleSet, error) {
	r := &RuleSet{}
	regexString := ``
	for _, ruleStr := range rulesStr {
		rule, err := ParseRule(ruleStr)

		switch {
		case err == nil:
			r.rules = append(r.rules, rule)
			if regexString == `` {
				regexString = rule.regexString
			} else {
				regexString = regexString + `|` + rule.regexString
			}
		case errors.Is(err, ErrSkipComment), errors.Is(err, ErrSkipHTML):
			logger.Info(err, ": ", ruleStr)
		default:
			logger.Info("cannot parse rule: ", err)
			return nil, fmt.Errorf("cannot parse rule: %w", err)
		}

	}
	r.regexString = regexString
	return r, nil
}

func (rule *ruleAdBlock) OptionsKeys() []string {
	opts := []string{}
	for option := range rule.options {
		opts = append(opts, option)
	}

	if rule.domains != nil && len(rule.domains) > 0 {
		opts = append(opts, "domain")
	}

	return opts
}

func parseDomainOption(text string) map[string]bool {
	domains := text[len("domain="):]
	parts := strings.Split(domains, "|")
	opts := make(map[string]bool, len(parts))

	for _, part := range parts {
		opts[strings.TrimPrefix(part, "~")] = !strings.HasPrefix(part, "~")
	}

	return opts
}

func ruleToRegexp(text string) string {
	// Convert AdBlock rule to a regular expression.
	if text == "" {
		return ".*"
	}

	// Check if the rule isn't already regexp
	length := len(text)
	if length >= 2 && text[:1] == "/" && text[length-1:] == "/" {
		return text[1 : length-1]
	}

	// escape special regex characters
	rule := text
	rule = regexp.QuoteMeta(rule)

	// |, ^ and * should not be escaped
	rule = strings.Replace(rule, `\|`, `|`, -1)
	rule = strings.Replace(rule, `\^`, `^`, -1)
	rule = strings.Replace(rule, `\*`, `*`, -1)

	// XXX: the resulting regex must use non-capturing groups (?:
	// for performance reasons; also, there is a limit on number
	// of capturing groups, no using them would prevent building
	// a single regex out of several rules.

	// Separator character ^ matches anything but a letter, a digit, or
	// one of the following: _ - . %. The end of the address is also
	// accepted as separator.
	rule = strings.Replace(rule, "^", `(?:[^\w\d_\\\-.%]|$)`, -1)

	// * symbol
	rule = strings.Replace(rule, "*", ".*", -1)

	// | in the end means the end of the address
	length = len(rule)
	if rule[length-1] == '|' {
		rule = rule[:length-1] + "$"
	}

	// || in the beginning means beginning of the domain name
	if rule[:2] == "||" {
		// XXX: it is better to use urlparse for such things,
		// but urlparse doesn't give us a single regex.
		// Regex is based on http://tools.ietf.org/html/rfc3986#appendix-B
		if len(rule) > 2 {
			//       |            | complete part       |
			//       |  scheme    | of the domain       |
			rule = `^(?:[^:/?#]+:)?(?://(?:[^/?#]*\.)?)?` + rule[2:]
		}
	} else if rule[0] == '|' {
		// | in the beginning means start of the address
		rule = "^" + rule[1:]
	}

	// other | symbols should be escaped
	// we have "|$" in our regexp - do not touch it
	rule = regexp.MustCompile(`(\|)[^$]`).ReplaceAllString(rule, `\|`)
	return rule
}
