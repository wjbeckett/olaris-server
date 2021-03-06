package parsers

import (
	log "github.com/sirupsen/logrus"
	"gitlab.com/olaris/olaris-server/metadata/helpers"
	"regexp"
	"strconv"
	"strings"
)

var yearRegex = regexp.MustCompile("([\\[\\(]?((?:19[0-9]|20[01])[0-9])[\\]\\)]?)")
var seriesRegex = regexp.MustCompile("^(.*)S(\\d{1,2})E(\\d{1,2})")
var seriesFallbackRegex = regexp.MustCompile("^(.*)(\\d{1,2})x(\\d{1,2})")

// ParsedSeriesInfo holds extracted information from the given filename.
type ParsedSeriesInfo struct {
	Year       uint64
	Title      string
	EpisodeNum int
	SeasonNum  int
}

func (psi *ParsedSeriesInfo) logFields() log.Fields {
	return log.Fields{"year": psi.Year, "title": psi.Title, "episodeNum": psi.EpisodeNum, "seasonNum": psi.SeasonNum}
}

// ParseSerieName attempts to parse a filename looking for episode/season information.
func ParseSerieName(fileName string) *ParsedSeriesInfo {
	log.WithFields(log.Fields{"filename": fileName}).Debugln("Parsing filename for episode information.")
	var err error
	var psi = ParsedSeriesInfo{}

	yearResult := yearRegex.FindStringSubmatch(fileName)
	if len(yearResult) > 1 {
		yearString := yearResult[2]
		log.WithFields(log.Fields{"year": yearString}).Println("Found release year.")
		// Remove Year data from original fileName
		fileName = strings.Replace(fileName, yearResult[1], "", -1)
		psi.Year, err = strconv.ParseUint(yearString, 10, 32)
		if err != nil {
			log.WithFields(log.Fields{"error": err}).Warnln("Could not convert year to uint")
		}
		log.WithFields(log.Fields{"filename": fileName}).Debugln("Removed year from episode information to create new title.", fileName)
	}

	// Find out episode numbers
	res := seriesRegex.FindStringSubmatch(fileName)
	if len(res) < 3 {
		// Fall back to some rarer used formats like 03x03 for season/episode
		res = seriesFallbackRegex.FindStringSubmatch(fileName)
	}

	// We expect a title, a season and an episode
	if len(res) > 2 {
		psi.SeasonNum, err = strconv.Atoi(res[2])
		if err != nil {
			log.Warnln("Could not convert season to uint:", err)
		}
		psi.EpisodeNum, err = strconv.Atoi(res[3])
		if err != nil {
			log.Warnln("Could not convert episode to uint:", err)
		}
		psi.Title = helpers.Sanitize(res[1])
	} else {
		psi.Title = helpers.Sanitize(fileName)
	}
	log.WithFields(psi.logFields()).Debugln("Done parsing episode.")

	return &psi
}
