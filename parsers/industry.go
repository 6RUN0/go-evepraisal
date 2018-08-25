package parsers

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Industry is the result from the industry parser
type Industry struct {
	Items []IndustryItem
	lines []int
}

// Name returns the parser name
func (r *Industry) Name() string {
	return "industry"
}

// Lines returns the lines that this result is made from
func (r *Industry) Lines() []int {
	return r.lines
}

// IndustryItem is a single item from an industry result
type IndustryItem struct {
	Name     string
	Quantity int64
	BPC      bool
	BPCRuns  int64
}

var reIndustry = regexp.MustCompile(`^([\S ]+) \(([\d]+) Units?\)$`)
var reIndustryBlueprints = regexp.MustCompile(strings.Join([]string{
	`^(?:([\d]+) x )?([\S\ ]+)`,           // Name
	`\t(-?[` + bigNumberRegex + `*)`,      // ME
	`\t(-?[` + bigNumberRegex + `*)`,      // TE
	`(?:\t(-?[` + bigNumberRegex + `*))?`, // ????
	`\t([` + bigNumberRegex + `*)`,        // Runs Remaining
	`(?:\t([\S ]*))?`,                     // Location
	`(?:\t([\S ]*))?`,                     // Location2
	`(?:\t([\S ]*))`,                      // Group
}, ""))

// ParseIndustry parses industry window text
func ParseIndustry(input Input) (ParserResult, Input) {
	industry := &Industry{}
	matches, rest := regexParseLines(reIndustry, input)
	industry.lines = append(industry.lines, regexMatchedLines(matches)...)

	matches2, rest := regexParseLines(reIndustryBlueprints, rest)
	industry.lines = append(industry.lines, regexMatchedLines(matches2)...)

	// collect items
	matchgroup := make(map[IndustryItem]int64)
	for _, match := range matches {
		matchgroup[IndustryItem{Name: match[1]}] += ToInt(match[2])
	}

	for _, match := range matches2 {
		runCount := ToInt(match[6])
		isBPC := false
		if runCount > 0 {
			isBPC = true
		}
		count := ToInt(match[1])
		if count == 0 {
			count = 1
		}
		matchgroup[IndustryItem{Name: match[2], BPC: isBPC, BPCRuns: runCount}] += count
	}

	// add items w/totals
	for item, quantity := range matchgroup {
		item.Quantity = quantity
		industry.Items = append(industry.Items, item)
	}

	sort.Slice(industry.Items, func(i, j int) bool {
		return fmt.Sprintf("%v", industry.Items[i]) < fmt.Sprintf("%v", industry.Items[j])
	})
	return industry, rest
}
