package engine

import (
	"fmt"
	"strings"
	"time"

	"retape_ai/internal/audio"
	"retape_ai/internal/config"
	"retape_ai/internal/detector"
)

type Signal struct {
	Type      string        // "beep", "silence", "phrase"
	Timestamp time.Duration
	Details   string
}

type Result struct {
	RecommendedDropTime time.Duration
	Reason              string
	Signals             []Signal
	Transcript          string
	DecisionMadeAt      time.Duration
	DeadAir             time.Duration
}

const PostBeepVerifyDuration = 500 * time.Millisecond

type DecisionEngine struct {
	config          *config.Config
	beepDetector    *detector.BeepDetector
	silenceDetector *detector.SilenceDetector
	phraseDetector  *detector.PhraseDetector
	stt             *detector.SpeechToText

	signals         []Signal
	transcript      string
	beepDetected    *detector.BeepEvent
	beepConfirmedAt time.Duration
	phraseFound     bool
	expectsBeep     bool
	phraseTime      time.Duration
	firstSilenceAt  time.Duration
	
	decisionMade    bool
	decisionResult  *Result
}

func NewDecisionEngine(cfg *config.Config, sampleRate int) *DecisionEngine {
	return &DecisionEngine{
		config:          cfg,
		beepDetector:    detector.NewBeepDetector(cfg, sampleRate),
		silenceDetector: detector.NewSilenceDetector(cfg),
		phraseDetector:  detector.NewPhraseDetector(cfg),
		stt:             detector.NewSpeechToText(cfg, sampleRate),
		signals:         make([]Signal, 0),
	}
}

func (e *DecisionEngine) Process(filePath string) (*Result, error) {
	streamer, err := audio.NewStreamer(filePath, e.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create streamer: %w", err)
	}

	sampleRate := streamer.SampleRate()
	e.beepDetector = detector.NewBeepDetector(e.config, sampleRate)
	e.stt = detector.NewSpeechToText(e.config, sampleRate)

	sttEnabled := false
	if e.stt.IsEnabled() {
		if err := e.stt.Connect(); err != nil {
			fmt.Printf("  [INFO] STT unavailable: %v\n", err)
		} else {
			sttEnabled = true
			defer e.stt.Close()
			go e.processTranscripts()
		}
	}

	var lastChunkTime time.Duration
	for chunk := range streamer.StreamWithPacing(sttEnabled) {
		lastChunkTime = chunk.Timestamp + chunk.Duration

		e.processChunk(chunk, sttEnabled)

		if e.decisionMade {
			break
		}

		e.checkForDecision(lastChunkTime)
	}

	if sttEnabled {
		time.Sleep(2 * time.Second) // Give Deepgram time to process
	}

	if !e.decisionMade {
		e.makeFinalDecision(lastChunkTime)
	}

	return e.decisionResult, nil
}

func (e *DecisionEngine) processChunk(chunk audio.AudioChunk, sttEnabled bool) {
	if beepEvent := e.beepDetector.Process(chunk); beepEvent != nil {
		e.beepDetected = beepEvent
		e.beepConfirmedAt = 0
		e.signals = append(e.signals, Signal{
			Type:      "beep",
			Timestamp: beepEvent.EndTime,
			Details:   fmt.Sprintf("freq=%.0fHz, duration=%v", beepEvent.Frequency, beepEvent.EndTime-beepEvent.StartTime),
		})
	}

	silenceEvent := e.silenceDetector.Process(chunk)
	if silenceEvent != nil {
		if silenceEvent.Confirmed && e.firstSilenceAt == 0 {
			e.firstSilenceAt = silenceEvent.StartTime
			e.signals = append(e.signals, Signal{
				Type:      "silence",
				Timestamp: silenceEvent.StartTime,
				Details:   fmt.Sprintf("confirmed silence, duration=%v", silenceEvent.Duration),
			})
		}
	}

	// Check if speech resumed after a beep (=> intermediate beep)
	if e.beepDetected != nil && e.beepConfirmedAt == 0 {
		timeSinceBeep := chunk.Timestamp - e.beepDetected.EndTime
		
		// If silence detector indicates speech is happening, reset the beep
		if timeSinceBeep > 0 && timeSinceBeep < PostBeepVerifyDuration {
			if silenceEvent == nil && !e.silenceDetector.IsInSilence() {
				e.signals = append(e.signals, Signal{
					Type:      "beep",
					Timestamp: chunk.Timestamp,
					Details:   "intermediate beep - speech resumed, ignoring",
				})
				e.beepDetected = nil
			}
		} else if timeSinceBeep >= PostBeepVerifyDuration {
			// Verify period passed with no speech - confirm this beep
			e.beepConfirmedAt = chunk.Timestamp
		}
	}

	if sttEnabled {
		e.stt.SendAudio(chunk.Samples)
	}
}

func (e *DecisionEngine) checkForDecision(currentTime time.Duration) {
	// Priority 1: Beep detected AND confirmed (verify period passed)
	if e.beepDetected != nil && e.beepConfirmedAt > 0 {
		e.makeDecision(
			e.beepDetected.EndTime+50*time.Millisecond,
			"Beep detected and confirmed (no speech resumed) - dropping after beep",
			currentTime,
		)
		return
	}

	// Priority 2: End phrase detected + confirmed silence = drop quickly
	if e.phraseFound && e.firstSilenceAt > 0 && !e.expectsBeep {
		// Phrase found, silence confirmed, no beep expected - drop after 1s wait
		timeSinceSilence := currentTime - e.firstSilenceAt
		if timeSinceSilence >= 1*time.Second {
			e.makeDecision(
				e.firstSilenceAt+200*time.Millisecond,
				"End phrase + silence detected (no beep expected) - dropping",
				currentTime,
			)
			return
		}
	}

	// Priority 3: Phrase indicates beep is coming - wait longer for beep
	if e.expectsBeep && e.firstSilenceAt > 0 {
		timeSinceSilence := currentTime - e.firstSilenceAt
		// Wait up to 5 seconds for beep when phrase says "after the beep"
		if timeSinceSilence >= 5*time.Second {
			e.makeDecision(
				e.firstSilenceAt+200*time.Millisecond,
				"Phrase indicated beep expected, waited 5s - dropping",
				currentTime,
			)
			return
		}
	}

	// Priority 4: Confirmed silence + timeout expired (no phrase indicating beep)
	// Skip this if we expect a beep - let Priority 3 handle the longer wait
	if e.firstSilenceAt > 0 && e.silenceDetector.HadSpeech() && !e.expectsBeep {
		timeSinceSilence := currentTime - e.firstSilenceAt
		if timeSinceSilence >= e.config.BeepWaitTimeout {
			e.makeDecision(
				e.firstSilenceAt+200*time.Millisecond,
				fmt.Sprintf("Confirmed silence, waited %.1fs for beep - dropping", e.config.BeepWaitTimeout.Seconds()),
				currentTime,
			)
			return
		}
	}
}

func (e *DecisionEngine) makeDecision(dropTime time.Duration, reason string, decisionTime time.Duration) {
	e.decisionMade = true
	
	var deadAir time.Duration
	if e.firstSilenceAt > 0 {
		deadAir = decisionTime - e.firstSilenceAt
	} else if e.beepDetected != nil {
		deadAir = decisionTime - e.beepDetected.EndTime
	}
	
	e.decisionResult = &Result{
		RecommendedDropTime: dropTime,
		Reason:              reason,
		Signals:             e.signals,
		Transcript:          e.transcript,
		DecisionMadeAt:      decisionTime,
		DeadAir:             deadAir,
	}
}

func (e *DecisionEngine) makeFinalDecision(totalDuration time.Duration) {
	var dropTime time.Duration
	var reason string

	if e.beepDetected != nil {
		dropTime = e.beepDetected.EndTime + 50*time.Millisecond
		reason = "Beep detected at end - dropping after beep"
	} else if e.firstSilenceAt > 0 && e.silenceDetector.HadSpeech() {
		dropTime = e.firstSilenceAt + 200*time.Millisecond
		reason = "Silence after speech - no beep detected"
	} else if e.phraseFound {
		phraseEvent := e.phraseDetector.GetDetected()
		if phraseEvent != nil {
			dropTime = phraseEvent.Timestamp + 1*time.Second
			reason = "End phrase detected"
		}
	} else {
		dropTime = time.Duration(float64(totalDuration) * 0.9)
		reason = "No clear signal - using fallback (90% of duration)"
	}

	var deadAir time.Duration
	if e.firstSilenceAt > 0 {
		deadAir = totalDuration - e.firstSilenceAt
	}
	
	e.decisionResult = &Result{
		RecommendedDropTime: dropTime,
		Reason:              reason,
		Signals:             e.signals,
		Transcript:          e.transcript,
		DecisionMadeAt:      totalDuration,
		DeadAir:             deadAir,
	}
}

func (e *DecisionEngine) processTranscripts() {
	for event := range e.stt.Results() {
		e.transcript += " " + event.Text

		if phraseEvent := e.phraseDetector.Process(event.Text, event.Timestamp); phraseEvent != nil {
			e.phraseFound = true
			if e.phraseTime == 0 {
				e.phraseTime = phraseEvent.Timestamp
			}
			
			phrase := strings.ToLower(phraseEvent.Phrase)
			if strings.Contains(phrase, "beep") || strings.Contains(phrase, "tone") {
				e.expectsBeep = true
			}
			
			e.signals = append(e.signals, Signal{
				Type:      "phrase",
				Timestamp: phraseEvent.Timestamp,
				Details:   fmt.Sprintf("matched: '%s'", phraseEvent.Phrase),
			})
		}
	}
}

func FormatResult(filename string, result *Result) string {
	output := fmt.Sprintf("\n=== %s ===\n", filename)

	if len(result.Signals) > 0 {
		output += "Detected signals:\n"
		for _, sig := range result.Signals {
			output += fmt.Sprintf("  - %s: %.2fs (%s)\n",
				sig.Type,
				sig.Timestamp.Seconds(),
				sig.Details)
		}
	} else {
		output += "Detected signals: None\n"
	}

	if result.Transcript != "" {
		transcript := result.Transcript
		if len(transcript) > 100 {
			transcript = transcript[:100] + "..."
		}
		output += fmt.Sprintf("Transcript: %s\n", transcript)
	}

	output += fmt.Sprintf("\n✓ Ideal drop time: %.2fs\n", result.RecommendedDropTime.Seconds())
	output += fmt.Sprintf("  Reason: %s\n", result.Reason)
	output += fmt.Sprintf("  Decision made at: %.2fs into stream\n", result.DecisionMadeAt.Seconds())
	if result.DeadAir > 0 {
		output += fmt.Sprintf("  Dead air: %.2fs\n", result.DeadAir.Seconds())
	}

	return output
}
