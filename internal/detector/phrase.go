package detector

import (
	"regexp"
	"strings"
	"time"
	"retape_ai/internal/config"
)

type PhraseEvent struct {
	Timestamp time.Duration
	Phrase    string
	FullText  string
}

type PhraseDetector struct {
	config   *config.Config
	patterns []*regexp.Regexp
	detected *PhraseEvent
}

func NewPhraseDetector(cfg *config.Config) *PhraseDetector {
	patterns := make([]*regexp.Regexp, len(cfg.EndPhrases))
	for i, phrase := range cfg.EndPhrases {
		patterns[i] = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(phrase))
	}

	return &PhraseDetector{
		config:   cfg,
		patterns: patterns,
	}
}

func (d *PhraseDetector) Process(text string, timestamp time.Duration) *PhraseEvent {
	text = strings.ToLower(text)

	for i, pattern := range d.patterns {
		if pattern.MatchString(text) {
			event := &PhraseEvent{
				Timestamp: timestamp,
				Phrase:    d.config.EndPhrases[i],
				FullText:  text,
			}
			d.detected = event
			return event
		}
	}

	return nil
}

func (d *PhraseDetector) GetDetected() *PhraseEvent {
	return d.detected
}
