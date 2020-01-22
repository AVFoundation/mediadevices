package opus

import (
	"fmt"
	"io"
	"math"
	"reflect"
	"unsafe"

	"github.com/faiface/beep"
	"github.com/pion/mediadevices/pkg/codec"
	"github.com/pion/mediadevices/pkg/io/audio"
	"github.com/pion/webrtc/v2"
	"gopkg.in/hraban/opus.v2"
)

type encoder struct {
	engine *opus.Encoder
	inBuff [][2]float32
	reader audio.Reader
}

var latencies = []float64{5, 10, 20, 40, 60}

var _ io.ReadCloser = &encoder{}
var _ codec.AudioEncoderBuilder = codec.AudioEncoderBuilder(NewEncoder)

func init() {
	codec.Register(webrtc.Opus, codec.AudioEncoderBuilder(NewEncoder))
}

func NewEncoder(r audio.Reader, s codec.AudioSetting) (io.ReadCloser, error) {
	if s.InSampleRate == 0 {
		return nil, fmt.Errorf("opus: InSampleRate is required")
	}

	if s.OutSampleRate == 0 {
		s.OutSampleRate = 48000
	}

	if s.Latency == 0 {
		s.Latency = 20
	}

	// Select the nearest supported latency
	var targetLatency float64
	nearestDist := math.Inf(+1)
	for _, latency := range latencies {
		dist := math.Abs(latency - s.Latency)
		if dist >= nearestDist {
			break
		}

		nearestDist = dist
		targetLatency = latency
	}

	// Since audio.Reader only supports stereo mode, channels is always 2
	channels := 2

	engine, err := opus.NewEncoder(s.OutSampleRate, channels, opus.AppVoIP)
	if err != nil {
		return nil, err
	}

	inBuffSize := targetLatency * float64(s.OutSampleRate) / 1000
	inBuff := make([][2]float32, int(inBuffSize))
	streamer := audio.ToBeep(r)
	newSampleRate := beep.SampleRate(s.OutSampleRate)
	oldSampleRate := beep.SampleRate(s.InSampleRate)
	streamer = beep.Resample(3, oldSampleRate, newSampleRate, streamer)

	reader := audio.FromBeep(streamer)
	e := encoder{engine, inBuff, reader}
	return &e, nil
}

func flatten(samples [][2]float32) []float32 {
	if len(samples) == 0 {
		return nil
	}

	data := uintptr(unsafe.Pointer(&samples[0]))
	l := len(samples) * 2
	return *(*[]float32)(unsafe.Pointer(&reflect.SliceHeader{Data: data, Len: l, Cap: l}))
}

func (e *encoder) Read(p []byte) (n int, err error) {
	var curN int

	// While the buffer is not full, keep reading so that we meet the latency requirement
	for curN < len(e.inBuff) {
		n, err := e.reader.Read(e.inBuff[curN:])
		if err != nil {
			return 0, err
		}

		curN += n
	}
	if err != nil {
		return 0, err
	}

	n, err = e.engine.EncodeFloat32(flatten(e.inBuff), p)
	if err != nil {
		return n, err
	}

	return n, nil
}

func (e *encoder) Close() error {
	return nil
}