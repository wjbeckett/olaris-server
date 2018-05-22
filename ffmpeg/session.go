package ffmpeg

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

type TranscodingSession struct {
	cmd            *exec.Cmd
	Stream         StreamRepresentation
	outputDir      string
	firstSegmentId int64
}

func (s *TranscodingSession) Start() error {
	if err := s.cmd.Start(); err != nil {
		return err
	}
	// Prevent zombies
	go func() {
		s.cmd.Wait()
	}()
	return nil
}

func (s *TranscodingSession) Destroy() error {
	// Signal the process group (-pid), not just the process, so that the process
	// and all its children are signaled. Else, child procs can keep running and
	// keep the stdout/stderr fd open and cause cmd.Wait to hang.
	syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
	// No error handling, we don't care if ffmpeg errors out, we're done here anyway.
	s.cmd.Wait()

	err := os.RemoveAll(s.outputDir)
	if err != nil {
		return err
	}

	return nil
}

// GetSegment return the filename of the given segment if it is projected to be available by the given deadline.
// It will block for at most deadline.
func (s *TranscodingSession) GetSegment(segmentId int64, deadline time.Duration) (string, error) {

	if !s.IsProjectedAvailable(segmentId, deadline) {
		return "", fmt.Errorf("Segment not projected to be available within deadline %s", deadline)
	}

	for {
		availableSegments, _ := s.AvailableSegments()
		if path, ok := availableSegments[segmentId]; ok {
			return path, nil
		}
		// TODO(Leon Handreke): Maybe a condition variable? Or maybe this blocking should move to the server module?
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *TranscodingSession) IsProjectedAvailable(segmentId int64, deadline time.Duration) bool {
	if s.Stream.Representation.RepresentationId == "webvtt" {
		return true
	}

	return s.firstSegmentId <= segmentId && segmentId < s.firstSegmentId+segmentsPerSession
}

func (s *TranscodingSession) AvailableSegments() (map[int64]string, error) {
	res := make(map[int64]string)

	files, err := ioutil.ReadDir(s.outputDir)
	if err != nil {
		return nil, err
	}

	r := regexp.MustCompile("stream0_(?P<number>\\d+).m4s")

	for _, f := range files {
		match := r.FindString(f.Name())
		if match != "" {
			segmentFsNumber, _ := strconv.Atoi(match[len("segment_") : len(match)-len(".m4s")])
			res[int64(segmentFsNumber)] = filepath.Join(s.outputDir, f.Name())
		}

	}

	return res, nil
}

// InitialSegment returns the path of the initial segment for the given stream
// or error if no initial segment is available for the given stream.
func (s *TranscodingSession) InitialSegment() string {
	return filepath.Join(s.outputDir, "init.mp4")
}
