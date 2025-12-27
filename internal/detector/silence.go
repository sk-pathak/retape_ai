package detector

import (
	"math"
	"time"
	"retape_ai/internal/audio"
	"retape_ai/internal/config"
)

type SilenceEvent struct {
	StartTime time.Duration
	EndTime   time.Duration
	Duration  time.Duration
	Confirmed bool
}

// SilenceDetector detects silence periods in audio using RMS analysis
// It distinguishes between brief speech pauses and actual greeting-end silence
type SilenceDetector struct {
	config          *config.Config
	silenceStart    time.Duration
	inSilence       bool
	hadSpeech       bool
	speechThreshold float64
	
	potentialEndTime    time.Duration
	confirmedEnd        bool
	lastSpeechTime      time.Duration
	speechAfterSilence  int
}

func NewSilenceDetector(cfg *config.Config) *SilenceDetector {
	return &SilenceDetector{
		config:          cfg,
		silenceStart:    0,
		inSilence:       false,
		hadSpeech:       false,
		speechThreshold: cfg.SilenceThreshold * 3, // Speech is significantly louder than silence
	}
}

func (d *SilenceDetector) Process(chunk audio.AudioChunk) *SilenceEvent {
	rms := calculateRMS(chunk.Samples)

	isSilent := rms < d.config.SilenceThreshold
	isSpeech := rms >= d.speechThreshold

	currentTime := chunk.Timestamp + chunk.Duration

	if isSpeech {
		d.hadSpeech = true
		d.lastSpeechTime = currentTime
		
		// If we were in a potential silence period and speech resumed,
		// this was just a pause, not greeting end
		if d.potentialEndTime > 0 && !d.confirmedEnd {
			d.speechAfterSilence++
		}
	}

	if isSilent {
		if !d.inSilence {
			// Start of silence
			d.silenceStart = chunk.Timestamp
			d.inSilence = true
		} else {
			// Check if silence is long enough
			elapsed := currentTime - d.silenceStart
			
			if d.hadSpeech && elapsed >= d.config.SilenceMinDur {
				// We have potential silence after speech
				if d.potentialEndTime == 0 {
					d.potentialEndTime = d.silenceStart
				}
				
				// Check if silence is sustained long enough to be "confirmed"
				// Sustained = at least 2 seconds of continuous silence
				sustainedDuration := currentTime - d.potentialEndTime
				if sustainedDuration >= 2*time.Second {
					d.confirmedEnd = true
				}
				
				return &SilenceEvent{
					StartTime: d.silenceStart,
					EndTime:   currentTime,
					Duration:  elapsed,
					Confirmed: d.confirmedEnd,
				}
			}
		}
	} else {
		// Sound detected, check if this breaks our silence
		if d.inSilence {
			silenceDuration := currentTime - d.silenceStart
			
			// If we had a short silence (< 2s) and speech resumed, reset
			if silenceDuration < 2*time.Second {
				d.potentialEndTime = 0
				d.confirmedEnd = false
				d.speechAfterSilence = 0
			}
		}
		d.inSilence = false
	}

	return nil
}

func (d *SilenceDetector) IsInSilence() bool {
	return d.inSilence
}

func (d *SilenceDetector) GetSilenceDuration(currentTime time.Duration) time.Duration {
	if !d.inSilence {
		return 0
	}
	return currentTime - d.silenceStart
}

func (d *SilenceDetector) HadSpeech() bool {
	return d.hadSpeech
}

func (d *SilenceDetector) IsConfirmedEnd() bool {
	return d.confirmedEnd
}

func (d *SilenceDetector) GetPotentialEndTime() time.Duration {
	return d.potentialEndTime
}

func calculateRMS(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}

	var sum float64
	for _, s := range samples {
		sum += s * s
	}

	return math.Sqrt(sum / float64(len(samples)))
}
