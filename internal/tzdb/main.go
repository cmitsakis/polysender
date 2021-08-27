package tzdb

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"time"
)

var (
	TimeZones              []string
	TimeZonesByCountryName map[string][]string
	TimeZonesByCountryCode map[string][]string
	Countries              []string
)

type timezoneTzdb struct {
	Name               string
	AlternativeName    string
	Group              []string
	ContinentCode      string
	ContinentName      string
	CountryName        string
	CountryCode        string
	MainCities         string
	RawOffsetInMinutes int
	Abbreviation       string
	RawFormat          string
}

func (tz timezoneTzdb) String() string {
	return fmt.Sprintf("%s %s", tz.Name, tz.CountryName)
}

func init() {
	var tzs []timezoneTzdb
	_ = json.Unmarshal([]byte(rawTimeZonesJSON), &tzs)
	countriesMap := make(map[string]struct{})
	TimeZonesByCountryName = make(map[string][]string)
	TimeZonesByCountryCode = make(map[string][]string)
	for _, tz := range tzs {
		_, err := time.LoadLocation(tz.Name)
		if err != nil {
			log.Println("unknown timezone:", tz)
			continue
		}
		countriesMap[tz.CountryName] = struct{}{}
		TimeZones = append(TimeZones, tz.String())
		TimeZonesByCountryName[tz.CountryName] = append(TimeZonesByCountryName[tz.CountryName], tz.String())
		TimeZonesByCountryCode[tz.CountryCode] = append(TimeZonesByCountryCode[tz.CountryCode], tz.String())
	}
	for country := range countriesMap {
		Countries = append(Countries, country)
	}
	sort.Strings(Countries)
}
