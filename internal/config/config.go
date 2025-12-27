package config

import (
	"os"
	"time"

	"github.com/joho/godotenv"
)

func init() {
	godotenv.Load()
}

type Config struct {
	// Audio processing settings
	ChunkDuration time.Duration
	SampleRate    int

	// Beep detection settings
	BeepMinFreq      float64
	BeepMaxFreq      float64
	BeepMinDuration  time.Duration
	BeepMinAmplitude float64

	// Silence detection settings
	SilenceThreshold float64
	SilenceMinDur    time.Duration

	// Real-time streaming settings
	BeepWaitTimeout time.Duration

	// Speech-to-text settings
	DeepgramAPIKey string
	EnableSTT      bool

	// End phrase patterns
	EndPhrases []string
}

func DefaultConfig() *Config {
	apiKey := os.Getenv("DEEPGRAM_API_KEY")

	return &Config{
		ChunkDuration: 20 * time.Millisecond,
		SampleRate:    16000,

		BeepMinFreq:      600.0,
		BeepMaxFreq:      2500.0,
		BeepMinDuration:  300 * time.Millisecond,
		BeepMinAmplitude: 0.02,
		SilenceThreshold: 0.01,
		SilenceMinDur:    500 * time.Millisecond,

		BeepWaitTimeout: 2 * time.Second,

		DeepgramAPIKey: apiKey,
		EnableSTT:      apiKey != "",

		// Common end-of-greeting phrases
		EndPhrases: []string{
			"after the beep",
			"after the tone",
			"leave a message",
			"leave your message",
			"leave your name",
			"leave your number",
			"record your message",
			"at the tone",
			"at the beep",
			"please leave",
			"brief message",
			"please leave a message",
			"you may leave",
			"record a message",
			"your message after",
		},
	}
}
