package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

type WAVHeader struct {
	ChunkID       [4]byte // "RIFF"
	ChunkSize     uint32
	Format        [4]byte // "WAVE"
	Subchunk1ID   [4]byte // "fmt "
	Subchunk1Size uint32
	AudioFormat   uint16
	NumChannels   uint16
	SampleRate    uint32
	ByteRate      uint32
	BlockAlign    uint16
	BitsPerSample uint16
}

type WAVFile struct {
	Header     WAVHeader
	DataOffset int64
	DataSize   uint32
	file       *os.File
}

func OpenWAV(path string) (*WAVFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	wav := &WAVFile{file: f}

	// Read header
	if err := binary.Read(f, binary.LittleEndian, &wav.Header); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to read WAV header: %w", err)
	}

	// Validate header
	if string(wav.Header.ChunkID[:]) != "RIFF" || string(wav.Header.Format[:]) != "WAVE" {
		f.Close()
		return nil, fmt.Errorf("not a valid WAV file")
	}

	fmtExtraBytes := int64(wav.Header.Subchunk1Size) - 16
	if fmtExtraBytes > 0 {
		if _, err := f.Seek(fmtExtraBytes, io.SeekCurrent); err != nil {
			f.Close()
			return nil, fmt.Errorf("failed to skip fmt extra bytes: %w", err)
		}
	}

	// Find data chunk
	for {
		var chunkID [4]byte
		var chunkSize uint32

		if err := binary.Read(f, binary.LittleEndian, &chunkID); err != nil {
			f.Close()
			return nil, fmt.Errorf("failed to find data chunk: %w", err)
		}
		if err := binary.Read(f, binary.LittleEndian, &chunkSize); err != nil {
			f.Close()
			return nil, fmt.Errorf("failed to read chunk size: %w", err)
		}

		if string(chunkID[:]) == "data" {
			wav.DataSize = chunkSize
			wav.DataOffset, _ = f.Seek(0, io.SeekCurrent)
			break
		}

		// Skip unknown chunk
		if _, err := f.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
			f.Close()
			return nil, fmt.Errorf("failed to skip chunk: %w", err)
		}
	}

	return wav, nil
}

func (w *WAVFile) Close() error {
	return w.file.Close()
}

func (w *WAVFile) SampleRate() int {
	return int(w.Header.SampleRate)
}

func (w *WAVFile) NumChannels() int {
	return int(w.Header.NumChannels)
}

func (w *WAVFile) BitsPerSample() int {
	return int(w.Header.BitsPerSample)
}

func (w *WAVFile) Duration() float64 {
	bytesPerSample := w.Header.BitsPerSample / 8
	samplesPerChannel := float64(w.DataSize) / float64(bytesPerSample) / float64(w.Header.NumChannels)
	return samplesPerChannel / float64(w.Header.SampleRate)
}

func (w *WAVFile) ReadSamples(numSamples int) ([]float64, error) {
	bytesPerSample := int(w.Header.BitsPerSample / 8)
	numChannels := int(w.Header.NumChannels)
	bytesToRead := numSamples * bytesPerSample * numChannels

	buf := make([]byte, bytesToRead)
	n, err := w.file.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}

	if n == 0 {
		return nil, io.EOF
	}

	actualSamples := n / (bytesPerSample * numChannels)
	samples := make([]float64, actualSamples)

	for i := 0; i < actualSamples; i++ {
		offset := i * bytesPerSample * numChannels
		var sample float64

		switch bytesPerSample {
		case 1: // 8-bit unsigned
			sample = (float64(buf[offset]) - 128) / 128.0
		case 2: // 16-bit signed
			val := int16(binary.LittleEndian.Uint16(buf[offset:]))
			sample = float64(val) / 32768.0
		case 4: // 32-bit signed
			val := int32(binary.LittleEndian.Uint32(buf[offset:]))
			sample = float64(val) / 2147483648.0
		}

		// average channels for stereo
		if numChannels == 2 {
			var sample2 float64
			offset2 := offset + bytesPerSample
			switch bytesPerSample {
			case 1:
				sample2 = (float64(buf[offset2]) - 128) / 128.0
			case 2:
				val := int16(binary.LittleEndian.Uint16(buf[offset2:]))
				sample2 = float64(val) / 32768.0
			case 4:
				val := int32(binary.LittleEndian.Uint32(buf[offset2:]))
				sample2 = float64(val) / 2147483648.0
			}
			sample = (sample + sample2) / 2
		}

		samples[i] = sample
	}

	return samples, nil
}

func (w *WAVFile) Reset() error {
	_, err := w.file.Seek(w.DataOffset, io.SeekStart)
	return err
}
