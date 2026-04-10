package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"github.com/gordonklaus/portaudio"
	"github.com/gorilla/websocket"
	"github.com/hraban/opus"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

var (
	voiceMembers     []VoiceMember
	voiceMembersList fyne.CanvasObject
)

type VoiceClient struct {
	ws            *websocket.Conn
	pc            map[string]*webrtc.PeerConnection // nick → peer connection
	mu            sync.Mutex
	myNick        string
	channel       string
	stream        *portaudio.Stream
	isConnected   bool
	isSpeaking    bool
	inputDevice   *portaudio.DeviceInfo
	outputDevice  *portaudio.DeviceInfo
	onSpeaking    func(nick string, speaking bool)
	onMemberJoin  func(nick string)
	onMemberLeave func(nick string)
	audioMu       sync.Mutex                       // protects stream, outputStream
	outputStream  *portaudio.Stream                // current output stream (owned by playAudio)
	outputGen     uint32                           // atomic; increment to tell playAudio to reopen output
	micTracks     []*webrtc.TrackLocalStaticSample // active mic tracks for RestartAudio
}

type SignalMsg struct {
	Type    string `json:"type"`
	From    string `json:"from"`
	To      string `json:"to"`
	Channel string `json:"channel"`
	Payload string `json:"payload"`
}

func ListAudioDevices() ([]*portaudio.DeviceInfo, error) {
	portaudio.Initialize()
	defer portaudio.Terminate()
	return portaudio.Devices()
}

func NewVoiceClient(myNick string) *VoiceClient {
	return &VoiceClient{
		myNick: myNick,
		pc:     make(map[string]*webrtc.PeerConnection),
	}
}

func (vc *VoiceClient) JoinChannel(wsAddr, channel string) error {
	portaudio.Initialize()
	if vc.inputDevice == nil {
		if d, err := portaudio.DefaultInputDevice(); err == nil {
			vc.inputDevice = d
		}
	}
	if vc.outputDevice == nil {
		if d, err := portaudio.DefaultOutputDevice(); err == nil {
			vc.outputDevice = d
		}
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsAddr+"/ws", nil)
	if err != nil {
		return fmt.Errorf("ws connect failed: %w", err)
	}
	vc.ws = conn
	vc.channel = channel
	vc.isConnected = true

	// send join message
	vc.send(SignalMsg{
		Type:    "join",
		From:    vc.myNick,
		Channel: channel,
	})

	// start reading signals
	go vc.readLoop()

	return nil
}

func (vc *VoiceClient) LeaveChannel() {
	if !vc.isConnected {
		return
	}
	// Set isConnected = false FIRST so all captureMic/playAudio goroutines
	// exit their loops on the next iteration check.
	vc.isConnected = false
	vc.send(SignalMsg{
		Type:    "leave",
		From:    vc.myNick,
		Channel: vc.channel,
	})
	vc.mu.Lock()
	pcs := make([]*webrtc.PeerConnection, 0, len(vc.pc))
	for _, pc := range vc.pc {
		pcs = append(pcs, pc)
	}
	vc.pc = make(map[string]*webrtc.PeerConnection)
	vc.micTracks = nil
	vc.mu.Unlock()
	// Close PCs in goroutines — TURN teardown can block for seconds.
	for _, pc := range pcs {
		go pc.Close()
	}
	// Close streams to unblock any goroutine currently blocked in stream.Read/Write.
	vc.audioMu.Lock()
	if vc.stream != nil {
		vc.stream.Stop()
		vc.stream.Close()
		vc.stream = nil
	}
	if vc.outputStream != nil {
		vc.outputStream.Stop()
		vc.outputStream.Close()
		vc.outputStream = nil
	}
	vc.audioMu.Unlock()
	vc.ws.Close()
	portaudio.Terminate()
}

// RestartAudio signals playAudio to reopen the output stream on the next packet
// (no abrupt close — avoids races with Write()), and restarts mic capture.
func (vc *VoiceClient) RestartAudio() {
	// Signal output switch. playAudio checks this after each Write() and reopens
	// gracefully with the new vc.outputDevice. We must NOT close vc.outputStream
	// here — that would race with an in-progress Write().
	atomic.AddUint32(&vc.outputGen, 1)

	// Restart mic input stream.
	vc.audioMu.Lock()
	if vc.stream != nil {
		vc.stream.Stop()
		vc.stream.Close()
		vc.stream = nil
	}
	vc.audioMu.Unlock()

	// Give old captureMic goroutine time to exit before relaunching.
	time.Sleep(80 * time.Millisecond)

	vc.mu.Lock()
	micTracks := make([]*webrtc.TrackLocalStaticSample, len(vc.micTracks))
	copy(micTracks, vc.micTracks)
	vc.mu.Unlock()
	for _, t := range micTracks {
		go vc.captureMic(t)
	}
}

func (vc *VoiceClient) send(msg SignalMsg) {
	data, _ := json.Marshal(msg)
	vc.mu.Lock()
	defer vc.mu.Unlock()
	if vc.ws == nil {
		return
	}
	vc.ws.WriteMessage(websocket.TextMessage, data)
}

func (vc *VoiceClient) readLoop() {
	for {
		_, data, err := vc.ws.ReadMessage()
		if err != nil {
			// WS closed — clean up any peers that didn't send a leave message.
			if vc.onMemberLeave != nil {
				vc.mu.Lock()
				peerNicks := make([]string, 0, len(vc.pc))
				for nick := range vc.pc {
					peerNicks = append(peerNicks, nick)
				}
				vc.mu.Unlock()
				for _, nick := range peerNicks {
					vc.onMemberLeave(nick)
				}
			}
			return
		}
		var msg SignalMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "offer":
			fmt.Println("VOICE: got offer from", msg.From)
			// Role tying: if our nick is lower, ignore this offer (we're the offerer)
			// Otherwise, accept it
			if vc.myNick < msg.From {
				fmt.Printf("VOICE: ignoring offer from %s (we have lower nick, we're the offerer)\n", msg.From)
				continue
			}
			go vc.handleOffer(msg.From, msg.Payload)
		case "answer":
			fmt.Println("VOICE: got answer from", msg.From)
			go vc.handleAnswer(msg.From, msg.Payload)
		case "ice":
			fmt.Println("VOICE: got ICE from", msg.From)
			go vc.handleICE(msg.From, msg.Payload)
		case "existing":
			// we are the new joiner — initiate call to existing member
			fmt.Println("VOICE: new peer joined:", msg.From)
			if vc.onMemberJoin != nil {
				vc.onMemberJoin(msg.From)
			}
			// Role tying: only initiate if our nick is lower (we're the offerer)
			if vc.myNick < msg.From {
				fmt.Printf("VOICE: initiating call to %s (we have lower nick)\n", msg.From)
				go vc.initiateCall(msg.From)
			} else {
				fmt.Printf("VOICE: not initiating call to %s (they have lower nick, they'll initiate)\n", msg.From)
			}
		case "join":
			fmt.Println("VOICE: peer joined, initiating call to", msg.From)
			if vc.onMemberJoin != nil {
				vc.onMemberJoin(msg.From)
			}
			// Role tying: only initiate if our nick is lower (we're the offerer)
			if vc.myNick < msg.From {
				fmt.Printf("VOICE: initiating call to %s (we have lower nick)\n", msg.From)
				go vc.initiateCall(msg.From)
			} else {
				fmt.Printf("VOICE: not initiating call to %s (they have lower nick, they'll initiate)\n", msg.From)
			}
		case "leave":
			if vc.onMemberLeave != nil {
				vc.onMemberLeave(msg.From)
			}
			vc.mu.Lock()
			pc, ok := vc.pc[msg.From]
			if ok {
				delete(vc.pc, msg.From)
			}
			vc.mu.Unlock()
			if ok {
				go pc.Close()
			}
		case "speaking":
			if vc.onSpeaking != nil {
				vc.onSpeaking(msg.From, msg.Payload == "speaking")
			}
		}
	}
}

func (vc *VoiceClient) newPeerConnection() (*webrtc.PeerConnection, error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{
					"stun:stun.l.google.com:19302",
					"stun:stun1.l.google.com:19302",
					"stun:stun2.l.google.com:19302",
					"stun:stun3.l.google.com:19302",
					"stun:stun4.l.google.com:19302",
				},
			},
			{
				URLs:       []string{"turn:freestun.net:3479"},
				Username:   "free",
				Credential: "free",
			},
			{
				URLs:       []string{"turns:freestun.net:5350"},
				Username:   "free",
				Credential: "free",
			},
		},
	}
	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	// Log when ICE gathering completes
	pc.OnICEGatheringStateChange(func(state webrtc.ICEGathererState) {
		fmt.Printf("VOICE: ICE gathering state changed to: %s\n", state.String())
	})

	return pc, nil
}

func (vc *VoiceClient) initiateCall(targetNick string) {
	pc, err := vc.newPeerConnection()
	if err != nil {
		fmt.Println("voice: create PC error:", err)
		return
	}

	vc.mu.Lock()
	vc.pc[targetNick] = pc
	vc.mu.Unlock()

	// Monitor connection state
	pc.OnConnectionStateChange(func(connectionState webrtc.PeerConnectionState) {
		fmt.Printf("VOICE: PC connection state changed to: %s (with %s)\n", connectionState.String(), targetNick)
	})

	pc.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("VOICE: ICE connection state changed to: %s (with %s)\n", connectionState.String(), targetNick)
	})

	// add audio track
	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio", "darkwave-audio",
	)
	if err != nil {
		fmt.Println("voice: create track error:", err)
		return
	}
	fmt.Println("VOICE: adding track to PC for", targetNick)
	pc.AddTrack(audioTrack)

	// track this for RestartAudio
	vc.mu.Lock()
	vc.micTracks = append(vc.micTracks, audioTrack)
	vc.mu.Unlock()
	// start mic capture to this track
	go vc.captureMic(audioTrack)

	// handle incoming audio
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		fmt.Println("VOICE: OnTrack fired, codec:", track.Codec().MimeType)
		go vc.playAudio(track)
	})

	// relay ICE candidates
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		data, _ := json.Marshal(c.ToJSON())
		vc.send(SignalMsg{
			Type:    "ice",
			From:    vc.myNick,
			To:      targetNick,
			Channel: vc.channel,
			Payload: string(data),
		})
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		fmt.Printf("VOICE: initiateCall - CreateOffer error: %v\n", err)
		return
	}
	err = pc.SetLocalDescription(offer)
	if err != nil {
		fmt.Printf("VOICE: initiateCall - SetLocalDescription error: %v\n", err)
		return
	}
	data, _ := json.Marshal(offer)
	vc.send(SignalMsg{
		Type:    "offer",
		From:    vc.myNick,
		To:      targetNick,
		Channel: vc.channel,
		Payload: string(data),
	})
}

func (vc *VoiceClient) handleOffer(fromNick, payload string) {
	vc.mu.Lock()
	existingPC, pcExists := vc.pc[fromNick]
	vc.mu.Unlock()

	var pc *webrtc.PeerConnection
	var err error

	// If we already have a connection with this peer, use it
	if pcExists {
		fmt.Println("VOICE: PC already exists for", fromNick, "- reusing it")
		pc = existingPC
	} else {
		// Create new peer connection only if it doesn't exist
		pc, err = vc.newPeerConnection()
		if err != nil {
			return
		}
		vc.mu.Lock()
		vc.pc[fromNick] = pc
		vc.mu.Unlock()

		// Monitor connection state
		pc.OnConnectionStateChange(func(connectionState webrtc.PeerConnectionState) {
			fmt.Printf("VOICE: PC connection state changed to: %s (with %s)\n", connectionState.String(), fromNick)
		})

		pc.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
			fmt.Printf("VOICE: ICE connection state changed to: %s (with %s)\n", connectionState.String(), fromNick)
		})

		// handle incoming audio
		pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			fmt.Println("VOICE: OnTrack fired, codec:", track.Codec().MimeType)
			go vc.playAudio(track)
		})

		// relay ICE candidates
		pc.OnICECandidate(func(c *webrtc.ICECandidate) {
			if c == nil {
				return
			}
			data, _ := json.Marshal(c.ToJSON())
			vc.send(SignalMsg{
				Type:    "ice",
				From:    vc.myNick,
				To:      fromNick,
				Channel: vc.channel,
				Payload: string(data),
			})
		})

		// add audio track
		audioTrack, err := webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
			"audio", "darkwave-audio",
		)
		if err != nil {
			return
		}
		fmt.Println("VOICE: adding track to PC for", fromNick)
		pc.AddTrack(audioTrack)
		// track this for RestartAudio
		vc.mu.Lock()
		vc.micTracks = append(vc.micTracks, audioTrack)
		vc.mu.Unlock()
		go vc.captureMic(audioTrack)
	}

	var offer webrtc.SessionDescription
	err = json.Unmarshal([]byte(payload), &offer)
	if err != nil {
		fmt.Printf("VOICE: handleOffer - unmarshal error: %v\n", err)
		return
	}
	fmt.Println("VOICE: handleOffer - setting remote description (offer) from", fromNick)
	err2 := pc.SetRemoteDescription(offer)
	if err2 != nil {
		fmt.Printf("VOICE: handleOffer - SetRemoteDescription error: %v\n", err2)
		return
	}

	answer, err3 := pc.CreateAnswer(nil)
	if err3 != nil {
		fmt.Printf("VOICE: handleOffer - CreateAnswer error: %v\n", err3)
		return
	}
	err4 := pc.SetLocalDescription(answer)
	if err4 != nil {
		fmt.Printf("VOICE: handleOffer - SetLocalDescription error: %v\n", err4)
		return
	}
	data, _ := json.Marshal(answer)
	vc.send(SignalMsg{
		Type:    "answer",
		From:    vc.myNick,
		To:      fromNick,
		Channel: vc.channel,
		Payload: string(data),
	})
}

func (vc *VoiceClient) handleAnswer(fromNick, payload string) {
	vc.mu.Lock()
	pc, ok := vc.pc[fromNick]
	vc.mu.Unlock()
	if !ok {
		return
	}
	var answer webrtc.SessionDescription
	err := json.Unmarshal([]byte(payload), &answer)
	if err != nil {
		fmt.Printf("VOICE: handleAnswer - unmarshal error: %v\n", err)
		return
	}
	fmt.Printf("VOICE: handleAnswer - setting remote description (answer) from %s\n", fromNick)
	err = pc.SetRemoteDescription(answer)
	if err != nil {
		fmt.Printf("VOICE: handleAnswer - SetRemoteDescription error: %v\n", err)
	}
}

func (vc *VoiceClient) handleICE(fromNick, payload string) {
	vc.mu.Lock()
	pc, ok := vc.pc[fromNick]
	vc.mu.Unlock()
	if !ok {
		return
	}
	var candidate webrtc.ICECandidateInit
	err := json.Unmarshal([]byte(payload), &candidate)
	if err != nil {
		fmt.Printf("VOICE: handleICE - unmarshal error: %v\n", err)
		return
	}
	err = pc.AddICECandidate(candidate)
	if err != nil {
		fmt.Printf("VOICE: handleICE - AddICECandidate error: %v\n", err)
	}
}

func (vc *VoiceClient) captureMic(track *webrtc.TrackLocalStaticSample) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("VOICE: captureMic PANIC: %v\n", r)
		}
	}()

	fmt.Println("VOICE: captureMic thread started for track:", track)
	sampleRate := 48000
	channels := 1
	framesPerBuffer := 480 // RNNoise requires 480
	buf := make([]int16, framesPerBuffer)
	floatBuf := make([]float32, 480)
	accumBuf := make([]int16, 960) // accumulate 2 chunks for opus
	accumCount := 0

	var lastSpeakingStatus bool
	var lastStatusTime time.Time

	var stream *portaudio.Stream
	var err error

	fmt.Println("VOICE: captureMic - opening input device...")
	if vc.inputDevice != nil {
		// use selected device
		fmt.Printf("VOICE: captureMic - using device: %v\n", vc.inputDevice.Name)
		params := portaudio.StreamParameters{
			Input: portaudio.StreamDeviceParameters{
				Device:   vc.inputDevice,
				Channels: channels,
				Latency:  vc.inputDevice.DefaultLowInputLatency,
			},
			SampleRate:      float64(sampleRate),
			FramesPerBuffer: framesPerBuffer,
		}
		stream, err = portaudio.OpenStream(params, buf)
	} else {
		fmt.Println("VOICE: captureMic - using default input device")
		stream, err = portaudio.OpenDefaultStream(channels, 0, float64(sampleRate), framesPerBuffer, buf)
	}
	if err != nil {
		fmt.Printf("VOICE: captureMic - stream open failed: %v\n", err)
		return
	}
	fmt.Println("VOICE: captureMic - stream opened successfully")
	vc.audioMu.Lock()
	vc.stream = stream
	vc.audioMu.Unlock()
	stream.Start()
	fmt.Println("VOICE: captureMic - stream started, entering read loop")
	defer stream.Stop()

	// create opus encoder
	fmt.Println("VOICE: captureMic - creating opus encoder...")
	enc, err := opus.NewEncoder(sampleRate, channels, 2048)
	if err != nil {
		fmt.Println("VOICE: captureMic - opus encoder creation failed:", err)
		return
	}
	fmt.Println("VOICE: captureMic - encoder created successfully")

	fmt.Println("VOICE: captureMic - initializing denoiser...")
	denoiser := NewDenoiser()
	if denoiser == nil {
		fmt.Println("VOICE: captureMic - denoiser initialization failed")
		return
	}
	fmt.Println("VOICE: captureMic - denoiser initialized")
	defer denoiser.Destroy()

	for vc.isConnected {
		readErr := stream.Read()
		if readErr != nil {
			fmt.Println("voice: read error (device disconnected?):", readErr)
			return
		}

		// convert int16 → float32
		for i, s := range buf {
			floatBuf[i] = float32(s)
		}

		// apply noise suppression
		vad := denoiser.Process(floatBuf)

		// convert back float32 → int16
		for i, f := range floatBuf {
			buf[i] = int16(f)
		}

		isVoice := vad > 0.0 // VAD threshold (disabled: always transmit for testing)

		if isVoice != lastSpeakingStatus && time.Since(lastStatusTime) > 200*time.Millisecond {
			lastSpeakingStatus = isVoice
			lastStatusTime = time.Now()
			vc.isSpeaking = isVoice
			status := "silent"
			if isVoice {
				status = "speaking"
			}
			vc.send(SignalMsg{
				Type:    "speaking",
				From:    vc.myNick,
				Channel: vc.channel,
				Payload: status,
			})
		}

		// VAD gate disabled for testing — transmit all frames unconditionally
		_ = isVoice

		// accumulate into 960-frame buffer
		copy(accumBuf[accumCount*480:], buf)
		accumCount++

		if accumCount < 2 {
			continue // wait for second chunk
		}
		accumCount = 0

		// encode full 960 frames
		opusBuf := make([]byte, 1000)
		n, err := enc.Encode(accumBuf, opusBuf)
		if err != nil {
			fmt.Printf("voice: encode error: %v\n", err)
			continue
		}
		fmt.Printf("voice: sending %d opus bytes\n", n)
		writeErr := track.WriteSample(media.Sample{
			Data:     opusBuf[:n],
			Duration: 20 * time.Millisecond,
		})
		if writeErr != nil {
			fmt.Printf("voice: WriteSample error: %v\n", writeErr)
		}
	}
}

func (vc *VoiceClient) playAudio(track *webrtc.TrackRemote) {
	fmt.Println("VOICE: playAudio started, codec:", track.Codec().MimeType)
	sampleRate := 48000
	channels := 1
	framesPerBuffer := 960
	buf := make([]int16, framesPerBuffer)

	// openStream closes any previous output stream and opens a fresh one.
	// Must be called only from the playAudio goroutine.
	openStream := func() *portaudio.Stream {
		vc.audioMu.Lock()
		defer vc.audioMu.Unlock()
		if vc.outputStream != nil {
			vc.outputStream.Stop()
			vc.outputStream.Close()
			vc.outputStream = nil
		}
		var s *portaudio.Stream
		var err error
		if vc.outputDevice != nil {
			fmt.Printf("VOICE: playAudio - opening device: %v\n", vc.outputDevice.Name)
			params := portaudio.StreamParameters{
				Output: portaudio.StreamDeviceParameters{
					Device:   vc.outputDevice,
					Channels: channels,
					Latency:  vc.outputDevice.DefaultLowOutputLatency,
				},
				SampleRate:      float64(sampleRate),
				FramesPerBuffer: framesPerBuffer,
			}
			s, err = portaudio.OpenStream(params, buf)
		} else {
			s, err = portaudio.OpenDefaultStream(0, channels, float64(sampleRate), framesPerBuffer, buf)
		}
		if err != nil {
			fmt.Printf("VOICE: playAudio - stream open failed: %v\n", err)
			return nil
		}
		s.Start()
		vc.outputStream = s
		fmt.Println("VOICE: playAudio - stream ready")
		return s
	}

	stream := openStream()
	if stream == nil {
		return
	}
	// Cleanup when this goroutine exits (e.g. RTP error on disconnect).
	// LeaveChannel may have already closed it; the pointer check makes it safe.
	defer func() {
		vc.audioMu.Lock()
		if vc.outputStream == stream {
			stream.Stop()
			stream.Close()
			vc.outputStream = nil
		}
		vc.audioMu.Unlock()
	}()

	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		fmt.Println("voice: opus decoder error:", err)
		return
	}

	// Track the generation we were opened at. RestartAudio increments outputGen
	// to request a device switch. We check after each Write() — no concurrent close.
	myGen := atomic.LoadUint32(&vc.outputGen)

	for vc.isConnected {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			fmt.Println("voice: RTP read error:", err)
			return
		}
		_, err = dec.Decode(pkt.Payload, buf)
		if err != nil {
			fmt.Println("voice: decode error:", err)
			continue
		}

		// Write to the current stream. No lock held — write is blocking (~20ms)
		// and RestartAudio never touches vc.outputStream, so there is no race.
		if err := stream.Write(); err != nil {
			fmt.Println("voice: write error, reopening:", err)
			// Treat a write error the same as a device switch request.
			atomic.AddUint32(&vc.outputGen, 1)
		}

		// After each write, check if a device switch was requested.
		curGen := atomic.LoadUint32(&vc.outputGen)
		if curGen != myGen {
			myGen = curGen
			newStream := openStream()
			if newStream == nil {
				return
			}
			stream = newStream
		}
	}
}
