package broadcast

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type SettingSendHours []TimeRange

func (h SettingSendHours) DBTable() string {
	return "settings"
}

func (h SettingSendHours) DBKey() []byte {
	return []byte("broadcast.send_hours")
}

func (h SettingSendHours) String() string {
	hStrings := make([]string, 0, len(h))
	for _, s := range h {
		hStrings = append(hStrings, s.String())
	}
	return strings.Join(hStrings, " ")
}

type SettingTimezone string

func (t SettingTimezone) DBTable() string {
	return "settings"
}

func (t SettingTimezone) DBKey() []byte {
	return []byte("broadcast.timezone")
}

var parseTimeRangesRe = regexp.MustCompile(`(([0-9]+)-([0-9]+))+`)

func ParseTimeRanges(str string) ([]TimeRange, error) {
	if !parseTimeRangesRe.MatchString(str) {
		return nil, fmt.Errorf("input is not properly formatted (e.g. '9-13 15-17')")
	}
	rMatches := parseTimeRangesRe.FindAllStringSubmatch(str, -1)
	rs := make([]TimeRange, 0)
	for _, rMatch := range rMatches {
		r, err := parseTimeRange(rMatch)
		if err != nil {
			return nil, fmt.Errorf("cannot parse '%v': %w", rMatch[0], err)
		}
		rs = append(rs, r)
	}
	return rs, nil
}

func parseTimeRange(match []string) (TimeRange, error) {
	if len(match) != 4 {
		return TimeRange{}, fmt.Errorf("invalid time range")
	}
	t1, err := strconv.Atoi(match[2])
	if err != nil {
		return TimeRange{}, fmt.Errorf("invalid time")
	}
	if t1 < 0 || t1 > 24 {
		return TimeRange{}, fmt.Errorf("time should be 0-24")
	}
	t2, err := strconv.Atoi(match[3])
	if err != nil {
		return TimeRange{}, fmt.Errorf("invalid time")
	}
	if t2 < 0 || t2 > 24 {
		return TimeRange{}, fmt.Errorf("time should be 0-24")
	}
	if t2 < t1 {
		return TimeRange{}, fmt.Errorf("end time is earlier than start time")
	}
	return TimeRange{
		From: time.Duration(t1) * time.Hour,
		To:   time.Duration(t2) * time.Hour,
	}, nil
}
