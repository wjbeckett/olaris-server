package dash

import (
	"bytes"
	"fmt"
	"gitlab.com/bytesized/bytesized-streaming/ffmpeg"
	"log"
	"text/template"
	"time"
)

const minSegDuration = time.Duration(5 * time.Second)

const transmuxingManifestTemplate = `<?xml version="1.0" encoding="utf-8"?>
<MPD xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
	xmlns="urn:mpeg:dash:schema:mpd:2011"
	xmlns:xlink="http://www.w3.org/1999/xlink"
	xsi:schemaLocation="urn:mpeg:dash:schema:mpd:2011 http://standards.iso.org/ittf/PubliclyAvailableStandards/MPEG-DASH_schema_files/DASH-MPD.xsd"
	profiles="urn:mpeg:dash:profile:isoff-live:2011"
	type="static"
	mediaPresentationDuration="{{ .duration }}"
	maxSegmentDuration="PT10S"
	minBufferTime="PT30S">
	<Period start="PT0S" id="0" duration="{{ .duration }}">
		<AdaptationSet contentType="video">
			<Representation
					id="direct-stream-video" mimeType="video/mp4"
					codecs="{{ .videoCodecSpecs }}"
					width="{{ .videoWidth }}"
					bandwidth="{{ .videoBitRate }}"
					height="{{ .videoHeight }}">
				<SegmentTemplate timescale="1000" initialization="$RepresentationID$/init.mp4" media="$RepresentationID$/$Number$.m4s" startNumber="0">
					<SegmentTimeline>
						{{ range $index, $duration := .segmentDurations }}
						<S {{ if eq $index 0}}t="0" {{ end }}d="{{ $duration }}"></S> <!-- {{ $index }} -->
						{{ end }}
					</SegmentTimeline>
				</SegmentTemplate>
			</Representation>
		</AdaptationSet>
		<AdaptationSet contentType="audio">
			<Representation id="direct-stream-audio" mimeType="audio/mp4" codecs="mp4a.40.2">
				<SegmentTemplate timescale="1000" initialization="$RepresentationID$/init.mp4" media="$RepresentationID$/$Number$.m4s" startNumber="0">
					<SegmentTimeline>
						{{ range $index, $duration := .segmentDurations }}
						<S {{ if eq $index 0}}t="0" {{ end }}d="{{ $duration }}"></S> <!-- {{ $index }} -->
						{{ end }}
					</SegmentTimeline>
				</SegmentTemplate>
            </Representation>
		</AdaptationSet>
	</Period>
</MPD>`

const transcodingManifestTemplate = `<?xml version="1.0" encoding="utf-8"?>
<MPD xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
	xmlns="urn:mpeg:dash:schema:mpd:2011"
	xmlns:xlink="http://www.w3.org/1999/xlink"
	xsi:schemaLocation="urn:mpeg:dash:schema:mpd:2011 http://standards.iso.org/ittf/PubliclyAvailableStandards/MPEG-DASH_schema_files/DASH-MPD.xsd"
	profiles="urn:mpeg:dash:profile:isoff-live:2011"
	type="static"
	mediaPresentationDuration="{{ .duration }}"
	maxSegmentDuration="PT10S"
	minBufferTime="PT30S">
	<Period start="PT0S" id="0" duration="{{ .duration }}">
		<AdaptationSet contentType="video">
			<Representation id="720p-1000k-video" mimeType="video/mp4" codecs="avc1.64001e" width="1024" height="552">
				<SegmentTemplate timescale="1000" duration="5000" initialization="$RepresentationID$/init.mp4" media="$RepresentationID$/$Number$.m4s" startNumber="0">
				</SegmentTemplate>
			</Representation>
		</AdaptationSet>
		<AdaptationSet contentType="audio">
			<Representation id="720p-1000k-audio" mimeType="audio/mp4" codecs="mp4a.40.2" bandwidth="128000" audioSamplingRate="48000">
				<SegmentTemplate timescale="1000" duration="5000" initialization="$RepresentationID$/init.mp4" media="$RepresentationID$/$Number$.m4s" startNumber="0">
				</SegmentTemplate>
			</Representation>
		</AdaptationSet>
	</Period>
</MPD>`

func BuildTransmuxingManifestFromFile(filePath string) string {
	probeData, err := ffmpeg.Probe(filePath)
	if err != nil {
		log.Fatal("Failed to ffprobe %s", filePath)
	}

	totalDuration := probeData.Format.Duration().Round(time.Millisecond)

	keyframes, err := ffmpeg.ProbeKeyframes(filePath)
	if err != nil {
		log.Fatal("Failed to ffprobe %s", filePath)
	}
	segmentDurations := ffmpeg.GuessSegmentDurations(keyframes, totalDuration, minSegDuration)
	durationXml := fmt.Sprintf("PT%dH%dM%d.%dS",
		totalDuration/time.Hour,
		(totalDuration%time.Hour)/time.Minute,
		(totalDuration%time.Minute)/time.Second,
		(totalDuration%time.Second)/time.Millisecond)

	// Segment durations in ms
	segmentDurationsMs := []int64{}
	for _, d := range segmentDurations {
		segmentDurationsMs = append(segmentDurationsMs, int64(d/time.Millisecond))

	}

	templateData := map[string]interface{}{
		"videoBitRate":     probeData.Streams[0].BitRate,
		"videoWidth":       probeData.Streams[0].Width,
		"videoHeight":      probeData.Streams[0].Height,
		"videoCodecSpecs":  probeData.Streams[0].GetMime(),
		"duration":         durationXml,
		"segmentDurations": segmentDurationsMs,
	}

	buf := bytes.Buffer{}
	t := template.Must(template.New("manifest").Parse(transmuxingManifestTemplate))
	t.Execute(&buf, templateData)
	return buf.String()
}

func BuildTranscodingManifestFromFile(filePath string) string {
	probeData, err := ffmpeg.Probe(filePath)
	if err != nil {
		log.Fatal("Failed to ffprobe %s", filePath)
	}
	totalDuration := probeData.Format.Duration().Round(time.Millisecond)

	durationXml := fmt.Sprintf("PT%dH%dM%d.%dS",
		totalDuration/time.Hour,
		(totalDuration%time.Hour)/time.Minute,
		(totalDuration%time.Minute)/time.Second,
		(totalDuration%time.Second)/time.Millisecond)

	templateData := map[string]interface{}{
		"duration": durationXml,
	}

	buf := bytes.Buffer{}
	t := template.Must(template.New("manifest").Parse(transcodingManifestTemplate))
	t.Execute(&buf, templateData)
	return buf.String()
}