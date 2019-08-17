package metadata

import (
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"gitlab.com/olaris/olaris-server/helpers/levenshtein"
	"gitlab.com/olaris/olaris-server/metadata/db"
	"gitlab.com/olaris/olaris-server/metadata/parsers"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

// TODO(Leon Handreke): Use a pool here and make it explicit in the documentation and function
//  name that we're only queueing these updates.
// ForceSeriesMetadataUpdate refreshes all data from the agent and updates the database record.
func (m *MetadataManager) ForceSeriesMetadataUpdate() {
	series, err := db.FindAllSeries(nil)
	if err != nil {

		log.WithField("error", err.Error()).
			Error("Failed to get series for forced metadata update")
	}
	for _, series := range series {
		m.UpdateSeriesMD(series)
		for _, season := range db.FindSeasonsForSeries(series.ID) {
			m.UpdateSeasonMD(&season)
			for _, episode := range db.FindEpisodesForSeason(season.ID) {
				m.UpdateEpisodeMD(&episode)
			}
		}
	}
}

// UpdateSeriesMD loops over all series with no tmdb information yet and attempts to retrieve the metadata.
func (m *MetadataManager) UpdateSeriesMD(series *db.Series) error {
	log.WithFields(log.Fields{"name": series.Name}).
		Println("Refreshing metadata for series.")
	m.agent.UpdateSeriesMD(series, series.TmdbID)
	db.SaveSeries(series)
	return nil
}

// UpdateEpisodeMD updates the database record with the latest data from the agent
func (m *MetadataManager) UpdateEpisodeMD(ep *db.Episode) error {
	m.agent.UpdateEpisodeMD(ep,
		ep.GetSeries().TmdbID, ep.GetSeason().SeasonNumber, ep.EpisodeNum)
	db.SaveEpisode(ep)
	return nil
}

// UpdateSeasonMD updates the database record with the latest data from the agent
func (m *MetadataManager) UpdateSeasonMD(season *db.Season) error {
	m.agent.UpdateSeasonMD(season, season.GetSeries().TmdbID, season.SeasonNumber)
	db.SaveSeason(season)
	return nil
}

// GetOrCreateEpisodeForEpisodeFile tries to create an Episode object by parsing the filename of the
// given EpisodeFile and looking it up in TMDB. It associates the EpisodeFile with the new Model.
// If no matching episode can be found in TMDB, it returns an error.
func (m *MetadataManager) GetOrCreateEpisodeForEpisodeFile(
	episodeFile *db.EpisodeFile) (*db.Episode, error) {

	if episodeFile.EpisodeID != 0 {
		return db.FindEpisodeByID(episodeFile.EpisodeID)
	}

	name := strings.TrimSuffix(episodeFile.FileName, filepath.Ext(episodeFile.FileName))
	parsedInfo := parsers.ParseSerieName(name)

	if parsedInfo.SeasonNum == 0 || parsedInfo.EpisodeNum == 0 {
		// We can't do anything if we don't know the season/episode number
		return nil, fmt.Errorf("Can't parse Season/Episode number from filename %s", name)
	}

	// Find a series for this Episode
	var options = make(map[string]string)
	if parsedInfo.Year != 0 {
		options["first_air_date_year"] = strconv.FormatUint(parsedInfo.Year, 10)
	}
	searchRes, err := m.agent.TmdbSearchTv(parsedInfo.Title, options)
	if err != nil {
		return nil, err
	}
	if len(searchRes.Results) == 0 {
		log.WithFields(log.Fields{
			"title": parsedInfo.Title,
			"year":  parsedInfo.Year,
		}).Warnln("Could not find match based on parsed title and given year.")

		return nil, errors.New("Could not find match in TMDB ID for given filename")
	}

	var bestDistance = math.MaxInt32
	// We use the index here because the type is really long.
	var bestResultIdx int
	for i, r := range searchRes.Results {
		d := levenshtein.ComputeDistance(parsedInfo.Title, r.Name)
		if d < bestDistance {
			bestDistance = d
			bestResultIdx = i
		}
	}
	seriesInfo := searchRes.Results[bestResultIdx]

	episode, err := m.GetOrCreateEpisodeByTmdbID(
		seriesInfo.ID, parsedInfo.SeasonNum, parsedInfo.EpisodeNum)
	if err != nil {
		return nil, err
	}

	episodeFile.Episode = episode
	episodeFile.EpisodeID = episode.ID
	db.SaveEpisodeFile(episodeFile)

	episode.EpisodeFiles = []db.EpisodeFile{*episodeFile}

	return episode, nil
}

// GetOrCreateEpisodeByTmdbID gets or creates an Episode object in the database,
// populating it with the details of the episode indicated by the TMDB ID.
func (m *MetadataManager) GetOrCreateEpisodeByTmdbID(
	seriesTmdbID int, seasonNum int, episodeNum int) (*db.Episode, error) {

	season, err := m.getOrCreateSeasonByTmdbID(seriesTmdbID, seasonNum)
	if err != nil {
		return nil, err
	}

	// Lock so that we don't create the same episode twice
	m.seriesMutex.Lock()
	defer m.seriesMutex.Unlock()

	episode, err := db.FindEpisodeByNumber(season, episodeNum)
	if err == nil {
		return episode, nil
	}

	episode = &db.Episode{Season: season, SeasonID: season.ID, EpisodeNum: episodeNum}
	if err := m.UpdateEpisodeMD(episode); err != nil {
		return nil, err
	}

	if m.Subscriber != nil {
		m.Subscriber.EpisodeAdded(episode)
	}

	return episode, nil
}

func (m *MetadataManager) getOrCreateSeriesByTmdbID(
	seriesTmdbID int) (*db.Series, error) {

	// Lock so that we don't create the same series twice
	m.seriesMutex.Lock()
	defer m.seriesMutex.Unlock()

	series, err := db.FindSeriesByTmdbID(seriesTmdbID)
	if err == nil {
		return series, nil
	}

	series = &db.Series{BaseItem: db.BaseItem{TmdbID: seriesTmdbID}}
	if err := m.UpdateSeriesMD(series); err != nil {
		return nil, err
	}

	if m.Subscriber != nil {
		m.Subscriber.SeriesAdded(series)
	}

	return series, nil
}

func (m *MetadataManager) getOrCreateSeasonByTmdbID(
	seriesTmdbID int, seasonNum int) (*db.Season, error) {

	series, err := m.getOrCreateSeriesByTmdbID(seriesTmdbID)
	if err != nil {
		return nil, err
	}

	// Lock so that we don't create the same series twice
	m.seriesMutex.Lock()
	defer m.seriesMutex.Unlock()

	season, err := db.FindSeasonBySeasonNumber(series, seasonNum)
	if err == nil {
		return season, nil
	}

	season = &db.Season{Series: series, SeriesID: series.ID, SeasonNumber: seasonNum}
	if err := m.UpdateSeasonMD(season); err != nil {
		return nil, err
	}

	if m.Subscriber != nil {
		m.Subscriber.SeasonAdded(season)
	}

	return season, nil
}

// GarbageCollectEpisodeIfRequired deletes an Episode and its associated Season/Series objects if
// required if no more EpisodeFiles associated with them remain.
func (m *MetadataManager) GarbageCollectEpisodeIfRequired(episode *db.Episode) error {
	if len(episode.EpisodeFiles) > 0 {
		return nil
	}

	db.DeleteEpisode(episode.ID)

	// Garbage collect season
	season, err := db.FindSeason(episode.SeasonID)
	if err != nil {
		return err
	}
	if len(season.Episodes) > 0 {
		return nil
	}
	err = db.DeleteSeason(season.ID)
	if err != nil {
		return err
	}

	// Garbage collect series
	series, err := db.FindSeries(season.SeriesID)
	if err != nil {
		return err
	}
	if len(series.Seasons) > 0 {
		return nil
	}
	err = db.DeleteSeries(series.ID)
	if err != nil {
		return err
	}

	return nil
}
