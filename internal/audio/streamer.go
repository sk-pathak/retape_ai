package audio

import (
	"time"
	"retape_ai/internal/config"
)

type AudioChunk struct {
	Samples   []float64
	Timestamp time.Duration
	Duration  time.Duration
}

type Streamer struct {
	wav           *WAVFile
	config        *config.Config
	currentTime   time.Duration
	samplesPerChunk int
}

func NewStreamer(wavPath string, cfg *config.Config) (*Streamer, error) {
	wav, err := OpenWAV(wavPath)
	if err != nil {
		return nil, err
	}

	sampleRate := wav.SampleRate()
	samplesPerChunk := int(float64(sampleRate) * cfg.ChunkDuration.Seconds())

	return &Streamer{
		wav:           wav,
		config:        cfg,
		currentTime:   0,
		samplesPerChunk: samplesPerChunk,
	}, nil
}

func (s *Streamer) StreamWithPacing(realTime bool) <-chan AudioChunk {
	ch := make(chan AudioChunk, 10)

	go func() {
		defer close(ch)
		defer s.wav.Close()

		for {
			samples, err := s.wav.ReadSamples(s.samplesPerChunk)
			if err != nil {
				return
			}

			if len(samples) == 0 {
				return
			}

			chunk := AudioChunk{
				Samples:   samples,
				Timestamp: s.currentTime,
				Duration:  s.config.ChunkDuration,
			}

			ch <- chunk

			actualDuration := time.Duration(float64(len(samples))/float64(s.wav.SampleRate())*1e9) * time.Nanosecond
			s.currentTime += actualDuration

			// sleep to simulate actual audio streaming
			if realTime {
				time.Sleep(actualDuration)
			}
		}
	}()

	return ch
}

func (s *Streamer) TotalDuration() time.Duration {
	return time.Duration(s.wav.Duration() * float64(time.Second))
}

func (s *Streamer) SampleRate() int {
	return s.wav.SampleRate()
}
