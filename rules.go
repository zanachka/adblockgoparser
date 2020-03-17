package adblockgoparser

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/google/logger"
)

var (
	ErrSkipComment     = errors.New("Commented rules are skipped")
	ErrSkipHTML        = errors.New("HTML rules are skipped")
	ErrUnsupportedRule = errors.New("Unsupported option rules are skipped")
	binaryOptions      = []string{
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
	// Except domain
	supportedOptions = []string{
		"image",
		"script",
		"stylesheet",
		"font",
		"thirdparty",
	}
	supportedOptionsPat = strings.Join(supportedOptions, ",")
	loggerInitialized   = false
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

func ParseRule(ruleText string) (*ruleAdBlock, error) {
	if !loggerInitialized {
		loggerInitialized = true
		logger.Init("ParseRule", true, true, ioutil.Discard)
		logger.SetFlags(log.LstdFlags)
	}
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
		parts := strings.SplitN(rule.ruleText, "$", 2)
		length := len(parts)

		if length > 0 {
			rule.ruleText = parts[0]
		}

		if length > 1 {
			for _, option := range strings.Split(parts[1], ",") {
				if strings.HasPrefix(option, "domain=") {
					rule.domains = parseDomainOption(option)
				} else {
					if ok := strings.Contains(supportedOptionsPat, option); !ok {
						logger.Info(ErrUnsupportedRule, ": ", option)
						return nil, ErrUnsupportedRule
					}
					rule.options[strings.TrimPrefix(option, "~")] = !strings.HasPrefix(option, "~")
				}
			}
		}
	}

	rule.regexString = ruleToRegexp(rule.ruleText)

	return rule, nil
}

type RuleSet struct {
	regexBasicString   string
	regexBasic         *regexp.Regexp
	rulesOptionsString map[string]string
	rulesOptionsRegex  map[string]*regexp.Regexp
}

func (ruleSet *RuleSet) Match(req Request) bool {
	did_match := false
	if ruleSet.regexBasic == nil {
		ruleSet.regexBasic = regexp.MustCompile(ruleSet.regexBasicString)
	}
	if ruleSet.regexBasicString != `` {
		did_match = ruleSet.regexBasic.MatchString(req.URL.String())
	}
	if did_match {
		return true
	}

	options := extractOptionsFromRequest(req)
	for option, active := range options {
		if active {
			if ruleSet.rulesOptionsRegex[option] == nil {
				ruleSet.rulesOptionsRegex[option] = regexp.MustCompile(ruleSet.rulesOptionsString[option])
			}
			if ruleSet.rulesOptionsString[option] != `` {
				did_match = ruleSet.rulesOptionsRegex[option].MatchString(req.URL.String())
			}
			if did_match {
				return true
			}
		}
	}
	return false
}

func (ruleSet *RuleSet) Allow(req Request) bool {
	return !ruleSet.Match(req)
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
	loggerInitialized = true

	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}
	return NewRuleSetFromStr(lines)
}

func NewRuleSetFromStr(rulesStr []string) (*RuleSet, error) {
	ruleSet := &RuleSet{
		rulesOptionsString: make(map[string]string, len(supportedOptions)),
		rulesOptionsRegex:  make(map[string]*regexp.Regexp, len(supportedOptions)),
	}
	// Init regex strings
	regexBasicString := ``
	for _, option := range supportedOptions {
		ruleSet.rulesOptionsString[option] = ``
	}

	// Start parsing
	for _, ruleStr := range rulesStr {
		rule, err := ParseRule(ruleStr)

		switch {
		case err == nil:
			if rule.options != nil && len(rule.options) > 0 {
				for option := range rule.options {
					if ruleSet.rulesOptionsString[option] == `` {
						ruleSet.rulesOptionsString[option] = rule.regexString
					} else {
						ruleSet.rulesOptionsString[option] = ruleSet.rulesOptionsString[option] + `|` + rule.regexString
					}
				}
			} else {
				if regexBasicString == `` {
					regexBasicString = rule.regexString
				} else {
					regexBasicString = regexBasicString + `|` + rule.regexString
				}
			}
		case errors.Is(err, ErrSkipComment), errors.Is(err, ErrSkipHTML), errors.Is(err, ErrUnsupportedRule):
			logger.Info(err, ": ", ruleStr)
		default:
			logger.Info("cannot parse rule: ", err)
			return nil, fmt.Errorf("cannot parse rule: %w", err)
		}
	}
	ruleSet.regexBasicString = regexBasicString
	return ruleSet, nil
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
	rule = strings.ReplaceAll(rule, `\|`, `|`)
	rule = strings.ReplaceAll(rule, `\^`, `^`)
	rule = strings.ReplaceAll(rule, `\*`, `*`)

	// XXX: the resulting regex must use non-capturing groups (?:
	// for performance reasons; also, there is a limit on number
	// of capturing groups, no using them would prevent building
	// a single regex out of several rules.

	// Separator character ^ matches anything but a letter, a digit, or
	// one of the following: _ - . %. The end of the address is also
	// accepted as separator.
	rule = strings.ReplaceAll(rule, "^", `(?:[^\w\d_\\\-.%]|$)`)

	// * symbol
	rule = strings.ReplaceAll(rule, "*", ".*")

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

func extractOptionsFromRequest(req Request) map[string]bool {
	result := make(map[string]bool, len(supportedOptions))

	filename := path.Base(req.URL.Path)
	result["script"] = regexp.MustCompile(`(?:\.js$|\.js\.gz$)`).MatchString(filename)
	result["image"] = regexp.MustCompile(`(?:\.jpg$|\.jpeg$|\.png$|\.gif$|\.webp$|\.tiff$|\.psd$|\.raw$|\.bmp$|\.heif$|\.indd$|\.jpeg2000$)`).MatchString(filename)
	result["stylesheet"] = regexp.MustCompile(`(?:\.css$)`).MatchString(filename)
	// More font extension at https://fileinfo.com/filetypes/font
	result["font"] = regexp.MustCompile(`(?:\.otf|\.ttf|\.fnt)`).MatchString(filename)
	result["thirdparty"] = req.Referer != ""

	return result
}
