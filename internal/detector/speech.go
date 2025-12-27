package detector

import (
	"context"
	"fmt"
	"time"

	"retape_ai/internal/config"

	api "github.com/deepgram/deepgram-go-sdk/pkg/api/listen/v1/websocket/interfaces"
	interfaces "github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces"
	client "github.com/deepgram/deepgram-go-sdk/pkg/client/listen"
)

type TranscriptEvent struct {
	Text      string
	Timestamp time.Duration
	IsFinal   bool
}

type SpeechToText struct {
	config     *config.Config
	sampleRate int
	dgClient   *client.WSCallback
	results    chan TranscriptEvent
	ctx        context.Context
	cancel     context.CancelFunc
	connected  bool
}

func NewSpeechToText(cfg *config.Config, sampleRate int) *SpeechToText {
	ctx, cancel := context.WithCancel(context.Background())
	return &SpeechToText{
		config:     cfg,
		sampleRate: sampleRate,
		results:    make(chan TranscriptEvent, 100),
		ctx:        ctx,
		cancel:     cancel,
	}
}

type messageHandler struct {
	stt *SpeechToText
}

func (h *messageHandler) Message(mr *api.MessageResponse) error {
	if len(mr.Channel.Alternatives) == 0 {
		return nil
	}

	transcript := mr.Channel.Alternatives[0].Transcript
	if transcript == "" {
		return nil
	}

	timestamp := time.Duration(mr.Start * float64(time.Second))

	event := TranscriptEvent{
		Text:      transcript,
		Timestamp: timestamp,
		IsFinal:   mr.IsFinal,
	}

	select {
	case h.stt.results <- event:
	default:
		// Channel full, skip
	}

	return nil
}

func (h *messageHandler) Open(ocr *api.OpenResponse) error {
	fmt.Println("  [STT] Connected to Deepgram")
	h.stt.connected = true
	return nil
}

func (h *messageHandler) Metadata(md *api.MetadataResponse) error {
	return nil
}

func (h *messageHandler) SpeechStarted(ssr *api.SpeechStartedResponse) error {
	return nil
}

func (h *messageHandler) UtteranceEnd(ur *api.UtteranceEndResponse) error {
	return nil
}

func (h *messageHandler) Close(ocr *api.CloseResponse) error {
	fmt.Println("  [STT] Disconnected from Deepgram")
	h.stt.connected = false
	return nil
}

func (h *messageHandler) Error(er *api.ErrorResponse) error {
	fmt.Printf("  [STT] Error: %s\n", er.Description)
	return nil
}

func (h *messageHandler) UnhandledEvent(byData []byte) error {
	return nil
}

func (s *SpeechToText) Connect() error {
	if !s.config.EnableSTT {
		return fmt.Errorf("speech-to-text is disabled (no API key)")
	}

	clientOptions := &interfaces.ClientOptions{
		APIKey: s.config.DeepgramAPIKey,
	}

	transcriptionOptions := &interfaces.LiveTranscriptionOptions{
		Model:          "nova-2",
		Language:       "en-US",
		Punctuate:      true,
		Encoding:       "linear16",
		SampleRate:     s.sampleRate,
		Channels:       1,
		InterimResults: true,
		SmartFormat:    true,
	}

	handler := &messageHandler{stt: s}

	dgClient, err := client.NewWSUsingCallback(s.ctx, "", clientOptions, transcriptionOptions, handler)
	if err != nil {
		return fmt.Errorf("failed to create Deepgram client: %w", err)
	}

	s.dgClient = dgClient

	wsConnected := s.dgClient.Connect()
	if !wsConnected {
		return fmt.Errorf("failed to connect to Deepgram WebSocket")
	}

	return nil
}

func (s *SpeechToText) SendAudio(samples []float64) error {
	if s.dgClient == nil || !s.connected {
		return nil // Silently skip if not connected (for no-stt)
	}

	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}

		val := int16(sample * 32767)
		data[i*2] = byte(val)
		data[i*2+1] = byte(val >> 8)
	}

	_, err := s.dgClient.Write(data)
	return err
}

func (s *SpeechToText) Results() <-chan TranscriptEvent {
	return s.results
}

func (s *SpeechToText) Close() {
	if s.dgClient != nil {
		s.dgClient.Stop()
	}
	s.cancel()
	close(s.results)
}

func (s *SpeechToText) IsEnabled() bool {
	return s.config.EnableSTT
}

func (s *SpeechToText) IsConnected() bool {
	return s.connected
}
