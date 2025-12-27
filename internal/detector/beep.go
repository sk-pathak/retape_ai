package detector

import (
	"math"
	"math/cmplx"
	"time"
	"retape_ai/internal/audio"
	"retape_ai/internal/config"
)

type BeepEvent struct {
	StartTime time.Duration
	EndTime   time.Duration
	Frequency float64
	Amplitude float64
}

type BeepDetector struct {
	config          *config.Config
	sampleRate      int
	beepStartTime   time.Duration
	beepActive      bool
	beepFrequency   float64
	beepAmplitude   float64
	consecutiveHits int
	minHits         int
	allBeeps        []*BeepEvent
}

func NewBeepDetector(cfg *config.Config, sampleRate int) *BeepDetector {
	minHits := int(150*time.Millisecond / cfg.ChunkDuration)
	if minHits < 5 {
		minHits = 5
	}

	return &BeepDetector{
		config:     cfg,
		sampleRate: sampleRate,
		minHits:    minHits,
		allBeeps:   make([]*BeepEvent, 0),
	}
}

func (d *BeepDetector) Process(chunk audio.AudioChunk) *BeepEvent {
	if len(chunk.Samples) < 64 {
		return nil
	}

	// Find dominant frequency and check if it's a tone-like signal
	freq, amp, isTone := d.analyzeForBeep(chunk.Samples)

	// Check if this matches beep criteria
	isBeepLike := freq >= d.config.BeepMinFreq &&
		freq <= d.config.BeepMaxFreq &&
		amp >= d.config.BeepMinAmplitude &&
		isTone

	// If we're already tracking a beep, check frequency consistency
	// A real beep maintains a consistent frequency, speech won't
	if isBeepLike && d.beepActive {
		freqDiff := math.Abs(freq-d.beepFrequency) / d.beepFrequency
		if freqDiff > 0.15 { // More than 15% frequency deviation => not a beep
			isBeepLike = false
		}
	}

	if isBeepLike {
		if !d.beepActive {
			// Potential start of beep
			d.beepStartTime = chunk.Timestamp
			d.beepFrequency = freq
			d.beepAmplitude = amp
		}
		d.consecutiveHits++
		d.beepActive = true
		// Update running average of frequency/amplitude
		d.beepFrequency = (d.beepFrequency*0.8 + freq*0.2) // Weighted average, favor existing
		d.beepAmplitude = math.Max(d.beepAmplitude, amp)
	} else {
		if d.beepActive && d.consecutiveHits >= d.minHits {
			event := &BeepEvent{
				StartTime: d.beepStartTime,
				EndTime:   chunk.Timestamp,
				Frequency: d.beepFrequency,
				Amplitude: d.beepAmplitude,
			}
			d.allBeeps = append(d.allBeeps, event)
			d.reset()
			return event
		}
		d.reset()
	}

	return nil
}


func (d *BeepDetector) reset() {
	d.beepActive = false
	d.consecutiveHits = 0
	d.beepFrequency = 0
	d.beepAmplitude = 0
}

// FFT to find the dominant frequency and determine if it's a tone
func (d *BeepDetector) analyzeForBeep(samples []float64) (float64, float64, bool) {
	// Pad to power of 2 for FFT efficiency
	n := nextPowerOf2(len(samples))
	if n < 128 {
		n = 128
	}
	padded := make([]complex128, n)
	for i, s := range samples {
		// Apply Hann window to reduce spectral leakage
		window := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(len(samples)-1)))
		padded[i] = complex(s*window, 0)
	}

	// Compute FFT
	fft := computeFFT(padded)

	// Find peak frequency in the beep range
	freqResolution := float64(d.sampleRate) / float64(n)
	minBin := int(d.config.BeepMinFreq / freqResolution)
	maxBin := int(d.config.BeepMaxFreq / freqResolution)

	if minBin < 1 {
		minBin = 1
	}
	if maxBin > n/2 {
		maxBin = n / 2
	}

	var maxMag float64
	var maxBinIdx int
	var totalMag float64

	for i := minBin; i <= maxBin; i++ {
		mag := cmplx.Abs(fft[i])
		totalMag += mag
		if mag > maxMag {
			maxMag = mag
			maxBinIdx = i
		}
	}

	dominantFreq := float64(maxBinIdx) * freqResolution
	// Normalize amplitude
	amplitude := maxMag / float64(n) * 2

	// Check if this is a pure tone by seeing if the peak dominates
	// A beep should have most energy concentrated in a narrow band
	// Require peak to be 5x above average (stricter than speech)
	avgMag := totalMag / float64(maxBin-minBin+1)
	isTone := maxMag > avgMag*5.0

	return dominantFreq, amplitude, isTone
}

func nextPowerOf2(n int) int {
	p := 1
	for p < n {
		p *= 2
	}
	return p
}

// computeFFT implements Cooley-Tukey FFT algorithm
func computeFFT(x []complex128) []complex128 {
	n := len(x)
	if n <= 1 {
		return x
	}

	// Bit-reversal permutation
	result := make([]complex128, n)
	copy(result, x)

	bits := 0
	for temp := n; temp > 1; temp >>= 1 {
		bits++
	}

	for i := 0; i < n; i++ {
		j := reverseBits(i, bits)
		if i < j {
			result[i], result[j] = result[j], result[i]
		}
	}

	// Cooley-Tukey iterative FFT
	for size := 2; size <= n; size *= 2 {
		halfSize := size / 2
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))

		for start := 0; start < n; start += size {
			wn := complex(1, 0)
			for k := 0; k < halfSize; k++ {
				t := wn * result[start+k+halfSize]
				u := result[start+k]
				result[start+k] = u + t
				result[start+k+halfSize] = u - t
				wn *= w
			}
		}
	}

	return result
}

func reverseBits(x, bits int) int {
	result := 0
	for i := 0; i < bits; i++ {
		result = (result << 1) | (x & 1)
		x >>= 1
	}
	return result
}
