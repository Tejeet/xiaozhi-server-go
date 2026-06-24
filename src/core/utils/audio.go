package utils

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hajimehoshi/go-mp3"
	opus "github.com/qrtc/opus-go"
)

// OpusDecoder wraps the opus decoder
type OpusDecoder struct {
	decoder   *opus.OpusDecoder
	mu        sync.Mutex
	config    *OpusDecoderConfig
	outBuffer []byte
}

// OpusDecoderConfig is the decoder configuration
type OpusDecoderConfig struct {
	SampleRate  int
	MaxChannels int
}

// NewOpusDecoder creates a new opus decoder
func NewOpusDecoder(config *OpusDecoderConfig) (*OpusDecoder, error) {
	if config == nil {
		config = &OpusDecoderConfig{
			SampleRate:  24000, // Use a 24kHz sample rate by default
			MaxChannels: 1,     // Mono by default
		}
	}

	libConfig := &opus.OpusDecoderConfig{
		SampleRate:  config.SampleRate,
		MaxChannels: config.MaxChannels,
	}

	decoder, err := opus.CreateOpusDecoder(libConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus decoder: %v", err)
	}

	bufSize := config.SampleRate * 2 * config.MaxChannels * 120 / 1000
	if bufSize < 8192 {
		bufSize = 8192 // At least an 8KB buffer
	}

	return &OpusDecoder{
		decoder:   decoder,
		config:    config,
		outBuffer: make([]byte, bufSize),
	}, nil
}

// Decode decodes opus data into PCM
func (d *OpusDecoder) Decode(opusData []byte) ([]byte, error) {
	if len(opusData) == 0 {
		return nil, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Use the pre-allocated buffer
	n, err := d.decoder.Decode(opusData, d.outBuffer)
	if err != nil {
		return nil, fmt.Errorf("Opus decode failed: %v", err)
	}

	// Return a copy of the decoded PCM data
	result := make([]byte, n)
	copy(result, d.outBuffer[:n])
	return result, nil
}

// Close closes the decoder
func (d *OpusDecoder) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.decoder != nil {
		if err := d.decoder.Close(); err != nil {
			return fmt.Errorf("failed to close Opus decoder: %v", err)
		}
		d.decoder = nil
	}
	return nil
}

func MP3ToPCMData(audioFile string) ([][]byte, error) {
	file, err := os.Open(audioFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open audio file: %v", err)
	}
	defer file.Close()

	decoder, err := mp3.NewDecoder(file)
	if err != nil {
		return nil, fmt.Errorf("failed to create MP3 decoder: %v", err)
	}

	mp3SampleRate := decoder.SampleRate()

	// Check whether the sample rate is supported
	supportedRates := map[int]bool{8000: true, 12000: true, 16000: true, 24000: true, 48000: true}
	if !supportedRates[mp3SampleRate] {
		return nil, fmt.Errorf("MP3 sample rate %dHz is not directly supported by Opus and needs resampling", mp3SampleRate)
	}

	// decoder.Length() returns the total number of bytes of decoded PCM data (16-bit little-endian stereo)
	pcmBytes := make([]byte, decoder.Length())
	// ReadFull ensures all requested bytes are read, otherwise it returns an error
	if _, err := io.ReadFull(decoder, pcmBytes); err != nil {
		// If decoder.Length() is 0, pcmBytes is empty, ReadFull reads 0 bytes and returns a nil error, which is normal.
		// If decoder.Length() > 0 and ReadFull returns an error, it means the complete PCM data could not be read.
		return nil, fmt.Errorf("failed to read PCM data: %v", err)
	}

	// go-mp3 decodes into 16-bit little-endian stereo PCM.
	// pcmBytes contains interleaved stereo data (LRLRLR...).
	// Each stereo sample pair (left 16-bit, right 16-bit) takes 4 bytes.
	// numMonoSamples is the number of 16-bit mono samples after conversion.
	numMonoSamples := len(pcmBytes) / 4

	if numMonoSamples == 0 {
		// Handle the case where pcmBytes is empty or there is not enough data for a single mono sample (i.e. fewer than 4 bytes).
		return [][]byte{}, nil // Return empty data
	}

	pcmMonoInt16 := make([]int16, numMonoSamples)
	for i := 0; i < numMonoSamples; i++ {
		// Extract the 16-bit little-endian left and right channel samples from pcmBytes
		// pcmBytes[i*4+0] = left channel low byte, pcmBytes[i*4+1] = left channel high byte
		// pcmBytes[i*4+2] = right channel low byte, pcmBytes[i*4+3] = right channel high byte
		leftSample := int16(uint16(pcmBytes[i*4+0]) | (uint16(pcmBytes[i*4+1]) << 8))
		rightSample := int16(uint16(pcmBytes[i*4+2]) | (uint16(pcmBytes[i*4+3]) << 8))

		// Mix down to a mono sample by averaging
		// Use int32 for the intermediate sum to prevent overflow before the division
		pcmMonoInt16[i] = int16((int32(leftSample) + int32(rightSample)) / 2)
	}

	// Convert the []int16 mono PCM data to []byte (still 16-bit little-endian)
	monoPcmDataBytes := make([]byte, numMonoSamples*2) // Each int16 sample takes 2 bytes
	for i, sample := range pcmMonoInt16 {
		monoPcmDataBytes[i*2] = byte(sample)        // Low byte (LSB)
		monoPcmDataBytes[i*2+1] = byte(sample >> 8) // High byte (MSB)
	}

	// The function signature requires returning [][]byte.
	// Return the entire mono PCM data as a single segment/slice in the outer slice.
	return [][]byte{monoPcmDataBytes}, nil
}

func SaveAudioToWavFile(
	data []byte,
	fileName string,
	sampleRate int,
	channels int,
	bitsPerSample int,
	append bool, // New parameter: whether to append, defaults to false
) (string, error) {
	// Handle the file name
	if fileName == "" {
		fileName = "output.wav"
	}

	var file *os.File
	var err error
	var currentDataSize int64 = 0

	// Check whether the file exists
	_, err = os.Stat(fileName)
	fileExists := !os.IsNotExist(err)

	if append && fileExists {
		// Append mode: open the existing file
		file, err = os.OpenFile(fileName, os.O_RDWR, 0644)
		if err != nil {
			return "", fmt.Errorf("failed to open file: %v", err)
		}
		defer file.Close()

		// Get the current data size
		fileInfo, err := file.Stat()
		if err != nil {
			return "", fmt.Errorf("failed to get file info: %v", err)
		}
		currentDataSize = fileInfo.Size() - 44 // Subtract the WAV header size (44 bytes)
		if currentDataSize < 0 {
			currentDataSize = 0
		}

		// Seek to the end of the file to prepare for appending data
		_, err = file.Seek(0, io.SeekEnd)
		if err != nil {
			return "", fmt.Errorf("failed to seek to end of file: %v", err)
		}
	} else {
		// Overwrite mode: delete the existing file (if any) and create a new one
		if fileExists {
			if err := os.Remove(fileName); err != nil {
				return "", fmt.Errorf("failed to delete existing file: %v", err)
			}
		}

		// Create a new file
		file, err = os.Create(fileName)
		if err != nil {
			return "", fmt.Errorf("failed to create file: %v", err)
		}
		defer file.Close()

		// Write the WAV file header
		if err := writeWavHeader(file, 0, sampleRate, channels, bitsPerSample); err != nil {
			return "", fmt.Errorf("failed to write WAV header: %v", err)
		}
	}

	// Open the existing file for appending
	file, err = os.OpenFile(fileName, os.O_WRONLY, 0o644)
	// Write the audio data
	_, err = file.Write(data)
	if err != nil {
		return "", fmt.Errorf("failed to write data: %v", err)
	}

	// Update the data size in the WAV header
	newDataSize := currentDataSize + int64(len(data))
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return "", fmt.Errorf("failed to seek to start of file: %v", err)
	}

	if err := writeWavHeader(file, int(newDataSize), sampleRate, channels, bitsPerSample); err != nil {
		return "", fmt.Errorf("failed to update WAV header: %v", err)
	}

	return fileName, nil
}

// writeWavHeader writes the WAV file header
func writeWavHeader(file *os.File, dataSize int, sampleRate, channels, bitsPerSample int) error {
	// RIFF chunk
	header := make([]byte, 44)
	copy(header[0:4], []byte("RIFF"))

	// Total file length = data size + header size(36) - 8
	fileSize := uint32(dataSize + 36)
	header[4] = byte(fileSize)
	header[5] = byte(fileSize >> 8)
	header[6] = byte(fileSize >> 16)
	header[7] = byte(fileSize >> 24)

	// File type
	copy(header[8:12], []byte("WAVE"))

	// Format chunk
	copy(header[12:16], []byte("fmt "))

	// Format chunk size (16 bytes)
	header[16] = 16
	header[17] = 0
	header[18] = 0
	header[19] = 0

	// Audio format (1 means PCM)
	header[20] = 1
	header[21] = 0

	// Number of channels
	header[22] = byte(channels)
	header[23] = 0

	// Sample rate
	header[24] = byte(sampleRate)
	header[25] = byte(sampleRate >> 8)
	header[26] = byte(sampleRate >> 16)
	header[27] = byte(sampleRate >> 24)

	// Byte rate = sample rate × channels × bit depth/8
	byteRate := uint32(sampleRate * channels * bitsPerSample / 8)
	header[28] = byte(byteRate)
	header[29] = byte(byteRate >> 8)
	header[30] = byte(byteRate >> 16)
	header[31] = byte(byteRate >> 24)

	// Block align = channels × bit depth/8
	blockAlign := uint16(channels * bitsPerSample / 8)
	header[32] = byte(blockAlign)
	header[33] = byte(blockAlign >> 8)

	// Bit depth
	header[34] = byte(bitsPerSample)
	header[35] = byte(bitsPerSample >> 8)

	// Data chunk
	copy(header[36:40], []byte("data"))

	// Data size
	header[40] = byte(dataSize)
	header[41] = byte(dataSize >> 8)
	header[42] = byte(dataSize >> 16)
	header[43] = byte(dataSize >> 24)

	_, err := file.Write(header)
	return err
}

// SaveAudioToFile keeps the original function but uses the new one
func SaveAudioToFile(data []byte, fileName string) (string, error) {
	// Default to 24kHz, mono, 16-bit
	return SaveAudioToWavFile(data, fileName, 24000, 1, 16, false)
}

func ReadPCMDataFromWavFile(filePath string) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAV file: %v", err)
	}
	defer file.Close()

	// Skip the WAV header
	header := make([]byte, 44)
	if _, err := file.Read(header); err != nil {
		return nil, fmt.Errorf("failed to read WAV header: %v", err)
	}

	// Read the PCM data
	pcmData, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read PCM data: %v", err)
	}

	return pcmData, nil
}

func AudioToPCMData(audioFile string) ([][]byte, float64, error) {
	file, err := os.Open(audioFile)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open audio file: %v", err)
	}
	defer file.Close()

	decoder, err := mp3.NewDecoder(file)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create MP3 decoder: %v", err)
	}

	mp3SampleRate := decoder.SampleRate()
	// fmt.Println("AudioToPCMData original MP3 sample rate:", mp3SampleRate)
	// Set the target sample rate to 24kHz
	targetSampleRate := 24000

	// decoder.Length() returns the total number of bytes of decoded PCM data (16-bit little-endian stereo)
	pcmBytes := make([]byte, decoder.Length())
	// ReadFull ensures all requested bytes are read, otherwise it returns an error
	if _, err := io.ReadFull(decoder, pcmBytes); err != nil {
		// If decoder.Length() is 0, pcmBytes is empty, ReadFull reads 0 bytes and returns a nil error, which is normal.
		// If decoder.Length() > 0 and ReadFull returns an error, it means the complete PCM data could not be read.
		return nil, 0, fmt.Errorf("failed to read PCM data: %v", err)
	}

	// go-mp3 decodes into 16-bit little-endian stereo PCM.
	// pcmBytes contains interleaved stereo data (LRLRLR...).
	// Each stereo sample pair (left 16-bit, right 16-bit) takes 4 bytes.
	// numMonoSamples is the number of 16-bit mono samples after conversion.
	numMonoSamples := len(pcmBytes) / 4

	if numMonoSamples == 0 {
		// Handle the case where pcmBytes is empty or there is not enough data for a single mono sample (i.e. fewer than 4 bytes).
		return [][]byte{}, 0, nil // Return empty data
	}

	pcmMonoInt16 := make([]int16, numMonoSamples)
	for i := 0; i < numMonoSamples; i++ {
		// Extract the 16-bit little-endian left and right channel samples from pcmBytes
		// pcmBytes[i*4+0] = left channel low byte, pcmBytes[i*4+1] = left channel high byte
		// pcmBytes[i*4+2] = right channel low byte, pcmBytes[i*4+3] = right channel high byte
		leftSample := int16(uint16(pcmBytes[i*4+0]) | (uint16(pcmBytes[i*4+1]) << 8))
		rightSample := int16(uint16(pcmBytes[i*4+2]) | (uint16(pcmBytes[i*4+3]) << 8))

		// Mix down to a mono sample by averaging
		// Use int32 for the intermediate sum to prevent overflow before the division
		pcmMonoInt16[i] = int16((int32(leftSample) + int32(rightSample)) / 2)
	}

	// Resample to the target sample rate (if needed)
	var resampledPcmInt16 []int16
	var finalSampleRate int

	if mp3SampleRate != targetSampleRate {
		fmt.Printf("Resampling from %dHz to %dHz\n", mp3SampleRate, targetSampleRate)
		resampledPcmInt16 = resamplePCM(pcmMonoInt16, mp3SampleRate, targetSampleRate)
		finalSampleRate = targetSampleRate
	} else {
		resampledPcmInt16 = pcmMonoInt16
		finalSampleRate = mp3SampleRate
	}

	// Convert the []int16 mono PCM data to []byte (still 16-bit little-endian)
	monoPcmDataBytes := make([]byte, len(resampledPcmInt16)*2) // Each int16 sample takes 2 bytes
	for i, sample := range resampledPcmInt16 {
		monoPcmDataBytes[i*2] = byte(sample)        // Low byte (LSB)
		monoPcmDataBytes[i*2+1] = byte(sample >> 8) // High byte (MSB)
	}

	// Audio playback duration (based on the resampled data)
	duration := float64(len(resampledPcmInt16)) / float64(finalSampleRate) // Duration of the mono PCM data (seconds)

	// The function signature requires returning [][]byte.
	// Return the entire mono PCM data as a single segment/slice in the outer slice.
	return [][]byte{monoPcmDataBytes}, duration, nil
}

// AudioToOpusData converts an audio file to Opus data chunks
func AudioToOpusData(audioFile string) ([][]byte, float64, error) {
	var pcmData [][]byte
	var err error
	var duration float64

	// Get the sample rate (fixed at 24000Hz as the Opus encoding sample rate)
	// If the sample rate is not 24000Hz, PCMSlicesToOpusData will handle the resampling
	opusSampleRate := 24000
	channels := 1

	if strings.HasSuffix(audioFile, ".mp3") {
		// First convert the MP3 to PCM
		pcmData, duration, err = AudioToPCMData(audioFile)
		if err != nil {
			return nil, 0, fmt.Errorf("PCM conversion failed: %v", err)
		}

		if len(pcmData) == 0 {
			return nil, 0, fmt.Errorf("PCM conversion result is empty")
		}

	} else {
		var singlePcmData []byte
		singlePcmData, _ = ReadPCMDataFromWavFile(audioFile)
		pcmData = [][]byte{singlePcmData}
	}

	// Convert the PCM to Opus
	opusData, err := PCMSlicesToOpusData(pcmData, opusSampleRate, channels, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("PCM-to-Opus conversion failed: %v", err)
	}

	return opusData, duration, nil
}

// CopyAudioFile copies an audio file
func CopyAudioFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

// SaveAudioFile saves audio data to a file
func SaveAudioFile(data []byte, filename string) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write audio data: %v", err)
	}

	return nil
}

// PCMToOpusData encodes PCM data into Opus format
func PCMToOpusData(pcmData []byte, sampleRate int, channels int) ([]byte, error) {
	if len(pcmData) == 0 {
		return nil, fmt.Errorf("PCM data is empty")
	}

	// Check whether the sample rate is supported
	supportedRates := map[int]bool{8000: true, 12000: true, 16000: true, 24000: true, 48000: true}
	if !supportedRates[sampleRate] {
		return nil, fmt.Errorf("sample rate %dHz is not supported by Opus; only 8000/12000/16000/24000/48000Hz are supported", sampleRate)
	}

	// Make sure the PCM data length is even, which is required for 16-bit PCM
	if len(pcmData)%2 != 0 {
		return nil, fmt.Errorf("PCM data length must be even (16-bit samples)")
	}

	// Convert the PCM bytes to int16 samples
	numSamples := len(pcmData) / 2 / channels
	pcmInt16 := make([]int16, numSamples*channels)
	for i := 0; i < numSamples*channels; i++ {
		// Read the little-endian 16-bit sample
		pcmInt16[i] = int16(uint16(pcmData[i*2]) | (uint16(pcmData[i*2+1]) << 8))
	}

	// Compute the number of samples per frame (60ms frame)
	samplesPerFrame := (sampleRate * 60) / 1000                         // 60ms frame
	framesCount := (numSamples + samplesPerFrame - 1) / samplesPerFrame // Round up

	// Adjust the sample array size based on the frame size
	paddedSampleCount := framesCount * samplesPerFrame
	if paddedSampleCount > numSamples {
		// Extend the sample array to the frame boundary
		paddedSamples := make([]int16, paddedSampleCount*channels)
		copy(paddedSamples, pcmInt16)
		pcmInt16 = paddedSamples
	}

	// Convert the int16 samples back to a byte array
	adjustedPcmData := make([]byte, len(pcmInt16)*2)
	for i, sample := range pcmInt16 {
		adjustedPcmData[i*2] = byte(sample)        // Low byte
		adjustedPcmData[i*2+1] = byte(sample >> 8) // High byte
	}

	// Create the Opus encoder
	encoder, err := opus.CreateOpusEncoder(&opus.OpusEncoderConfig{
		SampleRate:    sampleRate,
		MaxChannels:   channels,
		Application:   opus.AppVoIP,
		FrameDuration: opus.Framesize60Ms, // Use a 60ms frame length
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus encoder: %v", err)
	}
	defer encoder.Close()

	// Output buffer
	outBuf := make([]byte, 4096)

	// Encode the PCM data into Opus
	n, err := encoder.Encode(adjustedPcmData, outBuf)
	if err != nil {
		return nil, fmt.Errorf("Opus encoding failed: %v", err)
	}

	// Return the actually encoded data
	return outBuf[:n], nil
}

// PCMToOpusFile encodes PCM data into Opus and saves it to a file
func PCMToOpusFile(pcmData []byte, filename string, sampleRate int, channels int) error {
	opusData, err := PCMToOpusData(pcmData, sampleRate, channels)
	if err != nil {
		return err
	}

	return SaveAudioFile(opusData, filename)
}

// MP3ToOpusData converts an MP3 file to Opus format
func MP3ToOpusData(audioFile string) ([]byte, error) {
	// First convert the MP3 to PCM
	pcmDataSlices, err := MP3ToPCMData(audioFile)
	if err != nil {
		return nil, fmt.Errorf("MP3-to-PCM conversion failed: %v", err)
	}

	if len(pcmDataSlices) == 0 || len(pcmDataSlices[0]) == 0 {
		return nil, fmt.Errorf("PCM data is empty after MP3 decoding")
	}

	// Open the MP3 file to get the sample rate
	file, err := os.Open(audioFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open MP3 file: %v", err)
	}
	defer file.Close()

	decoder, err := mp3.NewDecoder(file)
	if err != nil {
		return nil, fmt.Errorf("failed to create MP3 decoder: %v", err)
	}

	// Get the sample rate
	sampleRate := decoder.SampleRate()
	fmt.Println("MP3 sample rate:", sampleRate)

	// Make sure the PCM data length is even
	pcmData := pcmDataSlices[0]
	if len(pcmData)%2 != 0 {
		return nil, fmt.Errorf("PCM data length must be even (16-bit samples)")
	}

	// Convert the PCM bytes to int16 samples
	numSamples := len(pcmData) / 2 // Mono
	pcmInt16 := make([]int16, numSamples)
	for i := 0; i < numSamples; i++ {
		// Read the little-endian 16-bit sample
		pcmInt16[i] = int16(uint16(pcmData[i*2]) | (uint16(pcmData[i*2+1]) << 8))
	}

	// Compute the number of samples per frame (60ms frame)
	samplesPerFrame := (sampleRate * 60) / 1000                         // 60ms frame
	framesCount := (numSamples + samplesPerFrame - 1) / samplesPerFrame // Round up

	// Adjust the sample array size based on the frame size
	paddedSampleCount := framesCount * samplesPerFrame
	if paddedSampleCount > numSamples {
		// Extend the sample array to the frame boundary
		paddedSamples := make([]int16, paddedSampleCount)
		copy(paddedSamples, pcmInt16)
		pcmInt16 = paddedSamples
	}

	// Convert the int16 samples back to a byte array
	adjustedPcmData := make([]byte, len(pcmInt16)*2)
	for i, sample := range pcmInt16 {
		adjustedPcmData[i*2] = byte(sample)        // Low byte
		adjustedPcmData[i*2+1] = byte(sample >> 8) // High byte
	}

	// Create the Opus encoder
	encoder, err := opus.CreateOpusEncoder(&opus.OpusEncoderConfig{
		SampleRate:    sampleRate,
		MaxChannels:   1, // Mono
		Application:   opus.AppVoIP,
		FrameDuration: opus.Framesize60Ms, // Use a 60ms frame length
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus encoder: %v", err)
	}
	defer encoder.Close()

	// Output buffer
	outBuf := make([]byte, 4096)

	// Encode the PCM data into Opus
	n, err := encoder.Encode(adjustedPcmData, outBuf)
	if err != nil {
		return nil, fmt.Errorf("Opus encoding failed: %v", err)
	}

	// Return the actually encoded data
	return outBuf[:n], nil
}

// MP3ToOpusFile converts an MP3 file to Opus and saves it to a file
func MP3ToOpusFile(inputFile, outputFile string, bitrate int) error {
	opusData, err := MP3ToOpusData(inputFile)
	if err != nil {
		return err
	}

	return SaveAudioFile(opusData, outputFile)
}

// PCMSlicesToOpusData batch-encodes PCM data slices into Opus format
func PCMSlicesToOpusData(pcmSlices [][]byte, sampleRate int, channels int, bitrate int) ([][]byte, error) {
	if len(pcmSlices) == 0 {
		return nil, fmt.Errorf("PCM data slices are empty")
	}

	// Check whether the sample rate is supported
	supportedRates := map[int]bool{8000: true, 12000: true, 16000: true, 24000: true, 48000: true}
	if !supportedRates[sampleRate] {
		return nil, fmt.Errorf("sample rate %dHz is not supported by Opus; only 8000/12000/16000/24000/48000Hz are supported", sampleRate)
	}

	// Create the Opus encoder
	encoder, err := opus.CreateOpusEncoder(&opus.OpusEncoderConfig{
		SampleRate:    sampleRate,
		MaxChannels:   channels,
		Application:   opus.AppVoIP,
		FrameDuration: opus.Framesize60Ms, // Use a 60ms frame length
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus encoder: %v", err)
	}
	defer encoder.Close()

	// All encoded Opus data packets
	var allOpusPackets [][]byte

	// Compute the number of samples per frame (60ms frame)
	samplesPerFrame := (sampleRate * 60) / 1000 // 60ms frame
	// Bytes per sample (16-bit = 2 bytes)
	bytesPerSample := 2 * channels
	// Bytes per frame
	bytesPerFrame := samplesPerFrame * bytesPerSample

	for _, pcmSlice := range pcmSlices {
		if len(pcmSlice) == 0 {
			continue
		}

		// Make sure the PCM data length is even
		if len(pcmSlice)%2 != 0 {
			pcmSlice = pcmSlice[:len(pcmSlice)-1] // Truncate the last byte
			if len(pcmSlice) == 0 {
				continue
			}
		}

		// Compute how many frames this PCM segment can be split into
		numFrames := len(pcmSlice) / bytesPerFrame
		if len(pcmSlice)%bytesPerFrame != 0 {
			numFrames++ // If there is leftover data, add one extra frame
		}

		// Process the PCM data frame by frame
		for frameIdx := 0; frameIdx < numFrames; frameIdx++ {
			frameStart := frameIdx * bytesPerFrame
			frameEnd := frameStart + bytesPerFrame

			// Make sure we don't go out of bounds
			if frameEnd > len(pcmSlice) {
				frameEnd = len(pcmSlice)
			}

			// The PCM data of the current frame
			framePcm := pcmSlice[frameStart:frameEnd]

			// If the last frame is incomplete, pad it with silence to the full frame size
			if len(framePcm) < bytesPerFrame {
				paddedFrame := make([]byte, bytesPerFrame)
				copy(paddedFrame, framePcm)
				framePcm = paddedFrame
			}

			// Allocate the output buffer (Opus-encoded data is usually smaller than PCM)
			outBuf := make([]byte, len(framePcm))

			// Encode this frame of PCM data into Opus
			n, err := encoder.Encode(framePcm, outBuf)
			if err != nil {
				continue // Skip this frame and continue with the next one
			}

			if n == 0 {
				continue // Skip empty frames
			}

			// Add the encoded Opus data to the result set
			allOpusPackets = append(allOpusPackets, outBuf[:n])
		}
	}

	if len(allOpusPackets) == 0 {
		return nil, fmt.Errorf("all PCM slices are empty after encoding")
	}

	return allOpusPackets, nil
}

// resamplePCM resamples PCM data using linear interpolation
func resamplePCM(input []int16, inputSampleRate, outputSampleRate int) []int16 {
	if inputSampleRate == outputSampleRate {
		return input
	}

	inputLength := len(input)
	if inputLength == 0 {
		return []int16{}
	}

	// Compute the resampling ratio
	ratio := float64(inputSampleRate) / float64(outputSampleRate)
	outputLength := int(float64(inputLength) / ratio)

	if outputLength == 0 {
		return []int16{}
	}

	output := make([]int16, outputLength)

	for i := 0; i < outputLength; i++ {
		// Compute the position in the input array
		srcIndex := float64(i) * ratio

		// Get the integer and fractional parts
		index := int(srcIndex)
		fraction := srcIndex - float64(index)

		if index >= inputLength-1 {
			// If out of bounds, use the last sample
			output[i] = input[inputLength-1]
		} else {
			// Linear interpolation
			sample1 := float64(input[index])
			sample2 := float64(input[index+1])
			interpolated := sample1 + fraction*(sample2-sample1)
			output[i] = int16(interpolated)
		}
	}

	return output
}
