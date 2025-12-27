# Voicemail Greeting End Detector

> **Task Assignment**: Detect the optimal moment to drop a compliant voicemail message after a customer's greeting ends.

A real-time audio processing system that identifies when a voicemail greeting ends by fusing beep detection, silence analysis, and speech-to-text signals. Built for production-like streaming scenarios where audio arrives chunk-by-chunk.

## Problem Summary

When an outbound call goes to voicemail, the system must determine exactly when to start playing a pre-recorded compliant message (containing company name and callback number). Starting too early risks cutting off the greeting; starting too late annoys the consumer.

**Challenges:**
- Greetings vary wildly in length and style
- Some end with a beep, some don't
- Natural speech has pauses that shouldn't trigger detection
- Must process audio as a stream (no buffering entire file)

## Quick Start

### Prerequisites
- Go 1.21+
- (Optional) [Deepgram API key](https://deepgram.com/) for speech-to-text

### Installation

```bash
# Clone and build
git clone <repo-url>
cd retape_ai
go build -o detector ./cmd/detector
```

### Running

```bash
# Create .env file with your Deepgram API key (optional)
echo "DEEPGRAM_API_KEY=your-api-key-here" > .env

# Run on all voicemail files with STT enabled
./detector -dir ./voicemails

# Run without STT (faster, beep/silence only)
./detector --no-stt -dir ./voicemails

# Single file
./detector -file ./voicemails/vm1.wav
```

## Architecture

Audio is streamed in 20ms chunks. Each chunk is processed by three detectors:

```
┌─────────────┐     ┌──────────────┐     ┌──────────────┐
│   Beep      │     │   Silence    │     │   Phrase     │
│  Detector   │     │  Detector    │     │  Detector    │
│  (FFT)      │     │  (RMS)       │     │  (STT)       │
└──────┬──────┘     └──────┬───────┘     └──────┬───────┘
       │                   │                    │
       └───────────────────┼────────────────────┘
                           ▼
                  ┌─────────────────┐
                  │ Decision Engine │
                  │ (Signal Fusion) │
                  └────────┬────────┘
                           ▼
                    Drop Timestamp
```

### Detection Methods

| Detector | Technique | Purpose |
|----------|-----------|---------|
| **Beep** | FFT frequency analysis (600-2500 Hz) | Definitive end signal |
| **Silence** | RMS amplitude with 2s sustained threshold | Fallback when no beep |
| **Phrase** | Pattern matching on STT transcripts | Context for wait times |

### Decision Priority

| Priority | Condition | Action |
|----------|-----------|--------|
| 1 | Beep detected | Drop immediately after beep ends |
| 2 | End phrase + silence (no beep mentioned) | Wait 1s, then drop |
| 3 | Phrase says "after the beep/tone" | Wait up to 5s for beep |
| 4 | Confirmed silence after speech | Wait 3s (configurable), then drop |

## Key Design Decisions

1. **Streaming over buffering**: Real phone calls stream audio—can't wait for call to end
2. **2-second sustained silence**: Prevents false triggers from natural speech pauses
3. **600 Hz lower bound for beep**: Avoids false positives from male voice fundamentals
4. **Priority-based logic**: Beeps are definitive; scoring systems could incorrectly downweight clear beeps

## Configuration

Configuration via `internal/config/config.go`:

| Parameter | Default | Description |
|-----------|---------|-------------|
| ChunkDuration | 20ms | Audio chunk size |
| BeepMinFreq | 600 Hz | Min beep frequency |
| BeepMaxFreq | 2500 Hz | Max beep frequency |
| BeepMinDuration | 300ms | Min beep length to confirm |
| SilenceThreshold | 0.01 | RMS threshold for silence |
| SilenceMinDur | 500ms | Min silence to start tracking |
| BeepWaitTimeout | 3s | Default wait after silence |

## Limitations & Trade-offs

- **Low-frequency beeps**: vm2 has a ~300 Hz beep below our threshold. We accept silence-based detection to avoid false positives from speech.
- **STT latency**: 0.5-2s delay from Deepgram. Could use local Whisper for lower latency.
- **Fixed phrases**: Pattern matching on predefined phrases. Could use LLM for semantic understanding.
