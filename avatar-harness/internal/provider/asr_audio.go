package provider

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
)

// asrPCM accepts the microphone contract and returns mono PCM S16LE at 16 kHz.
// WAV metadata is authoritative: a WAV container must never be sent as raw PCM.
func asrPCM(input map[string]any) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(String(input["audio_base64"]))
	if err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("ASR 需要有效的 Base64 音频")
	}
	rate, channels := int(Number(input["sample_rate"])), int(Number(input["channels"]))
	if channels == 0 {
		channels = 1
	}
	switch String(input["format"]) {
	case "wav":
		if len(raw) < 12 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
			return nil, fmt.Errorf("ASR WAV 文件头无效")
		}
		var data []byte
		validFormat := false
		for offset := 12; offset+8 <= len(raw); {
			size := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
			if size > len(raw)-offset-8 {
				return nil, fmt.Errorf("ASR WAV 数据截断")
			}
			chunk := raw[offset+8 : offset+8+size]
			switch string(raw[offset : offset+4]) {
			case "fmt ":
				if size < 16 || binary.LittleEndian.Uint16(chunk[:2]) != 1 || binary.LittleEndian.Uint16(chunk[14:16]) != 16 {
					return nil, fmt.Errorf("ASR WAV 只支持 16 位 PCM")
				}
				channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
				rate = int(binary.LittleEndian.Uint32(chunk[4:8]))
				validFormat = true
			case "data":
				data = append(data, chunk...)
			}
			offset += 8 + size + size%2
		}
		if !validFormat || len(data) == 0 {
			return nil, fmt.Errorf("ASR WAV 缺少 fmt/data")
		}
		raw = data
	case "pcm_s16le", "pcm":
	default:
		return nil, fmt.Errorf("百炼 ASR 输入只支持 WAV 或 PCM S16LE")
	}
	if rate < 8000 || rate > 192000 || (channels != 1 && channels != 2) || len(raw)%(channels*2) != 0 {
		return nil, fmt.Errorf("ASR 音频采样率、声道或长度无效")
	}
	frames := len(raw) / (channels * 2)
	if frames < rate/100 || frames > rate*60 {
		return nil, fmt.Errorf("ASR 每次输入时长须在 10ms–60s 之间")
	}
	if rate == 16000 && channels == 1 {
		return raw, nil
	}
	mono := make([]float64, frames)
	for index := range mono {
		for channel := 0; channel < channels; channel++ {
			pos := (index*channels + channel) * 2
			mono[index] += float64(int16(binary.LittleEndian.Uint16(raw[pos:pos+2]))) / float64(channels)
		}
	}
	count := frames * 16000 / rate
	result := make([]byte, count*2)
	ratio := float64(rate) / 16000
	for index := 0; index < count; index++ {
		start, end := float64(index)*ratio, float64(index+1)*ratio
		var sample float64
		if ratio >= 1 {
			// Area averaging attenuates high frequencies during downsampling.
			for source := int(start); source < int(math.Ceil(end)) && source < frames; source++ {
				weight := math.Min(end, float64(source+1)) - math.Max(start, float64(source))
				sample += mono[source] * weight / ratio
			}
		} else {
			left := int(start)
			fraction := start - float64(left)
			sample = mono[left]*(1-fraction) + mono[min(left+1, frames-1)]*fraction
		}
		binary.LittleEndian.PutUint16(result[index*2:], uint16(int16(math.Round(sample))))
	}
	return result, nil
}
