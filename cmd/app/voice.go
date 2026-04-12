package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
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
	ws               *websocket.Conn
	pc               map[string]*webrtc.PeerConnection // nick → peer connection
	mu               sync.Mutex
	myNick           string
	channel          string
	stream           *portaudio.Stream
	isConnected      bool
	isSpeaking       bool
	inputDeviceName  string // store device name, not pointer (pointers become stale after Initialize/Terminate)
	outputDeviceName string // store device name, not pointer (pointers become stale after Initialize/Terminate)
	onSpeaking       func(nick string, speaking bool)
	onMemberJoin     func(nick string)
	onMemberLeave    func(nick string)
	audioMu          sync.Mutex                       // protects stream, outputStream
	outputStream     *portaudio.Stream                // central output stream (owned by playbackManager)
	micTracks        []*webrtc.TrackLocalStaticSample // active mic tracks for RestartAudio

	// Audio playback management
	audioQueue chan []int16 // queue of audio samples from all peers

	// AGC (Automatic Gain Control)
	agcTargetLevel float32 // target RMS level (0.1 to 0.5 typically)
	agcCurrentGain float32 // current gain multiplier

	// ICE candidate buffering (for race condition fix)
	iceCandidateQueues map[string][]*webrtc.ICECandidateInit // nick → queued candidates
	iceQueueMu         sync.Mutex

	// Per-peer volume adjustment (1.0 = 100%, range 0.0-2.0)
	peerVolumes map[string]float32 // nick → volume multiplier
	volumeMu    sync.Mutex
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
		myNick:             myNick,
		pc:                 make(map[string]*webrtc.PeerConnection),
		agcTargetLevel:     5000, // target RMS in int16 scale (lowered to prevent echo amplification)
		agcCurrentGain:     1.0,  // start with unity gain
		iceCandidateQueues: make(map[string][]*webrtc.ICECandidateInit),
		peerVolumes:        make(map[string]float32), // initialize peer volumes map
		audioQueue:         make(chan []int16, 10),   // buffered channel for peer audio
	}
}

func (vc *VoiceClient) JoinChannel(wsAddr, channel string) error {
	portaudio.Initialize()

	// Recreate audioQueue in case it was closed on previous LeaveChannel
	vc.audioQueue = make(chan []int16, 10)

	if vc.inputDeviceName == "" {
		if d, err := portaudio.DefaultInputDevice(); err == nil {
			vc.inputDeviceName = d.Name
		}
	}
	if vc.outputDeviceName == "" {
		if d, err := portaudio.DefaultOutputDevice(); err == nil {
			vc.outputDeviceName = d.Name
		}
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsAddr+"/ws", nil)
	if err != nil {
		return fmt.Errorf("ws connect failed: %w", err)
	}
	vc.ws = conn
	vc.channel = channel
	vc.isConnected = true

	// start playback manager to handle audio from all peers
	go vc.playbackManager()

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
	// Set isConnected = false FIRST so all captureMic/playAudio/playbackManager goroutines
	// exit their loops on the next iteration check.
	vc.isConnected = false

	// Close audioQueue to unblock playbackManager and playAudio goroutines
	close(vc.audioQueue)

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

	// Clear ICE candidate queues to free memory
	vc.iceQueueMu.Lock()
	vc.iceCandidateQueues = make(map[string][]*webrtc.ICECandidateInit)
	vc.iceQueueMu.Unlock()

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

	// Close WebSocket to unblock readLoop
	vc.mu.Lock()
	if vc.ws != nil {
		vc.ws.Close()
		vc.ws = nil
	}
	vc.mu.Unlock()

	// PortAudio Terminate only once at app shutdown, not on each channel leave
	// Removed: portaudio.Terminate()
}

// playbackManager owns the single output stream for the entire session
// All peer audio is sent to this manager via the audioQueue channel
func (vc *VoiceClient) playbackManager() {
	fmt.Println("VOICE: playbackManager started")
	sampleRate := 48000
	channels := 1
	framesPerBuffer := 960
	buf := make([]int16, framesPerBuffer)

	// Open output stream once for the entire session
	vc.audioMu.Lock()
	if vc.outputStream != nil {
		vc.outputStream.Stop()
		vc.outputStream.Close()
	}

	// Log all available output devices for debugging
	if devices, err := portaudio.Devices(); err == nil {
		fmt.Printf("VOICE: playbackManager - available output devices: %d\n", len(devices))
		for i, d := range devices {
			if d.MaxOutputChannels > 0 {
				fmt.Printf("  [%d] %s (max channels: %d)\n", i, d.Name, d.MaxOutputChannels)
			}
		}
	}

	var s *portaudio.Stream
	var err error
	var outputDev *portaudio.DeviceInfo

	// Look up device by name using prefix matching (handles truncated names)
	fmt.Printf("VOICE: playbackManager - looking for output device by prefix: '%s'\n", vc.outputDeviceName)
	if vc.outputDeviceName != "" {
		if devices, err := portaudio.Devices(); err == nil {
			fmt.Printf("VOICE: playbackManager - searching %d devices for match\n", len(devices))
			for _, d := range devices {
				if d.MaxOutputChannels > 0 {
					// Use prefix matching to handle truncated device names
					if strings.HasPrefix(vc.outputDeviceName, d.Name) || strings.HasPrefix(d.Name, vc.outputDeviceName) {
						fmt.Printf("VOICE: playbackManager - MATCH FOUND: '%s'\n", d.Name)
						outputDev = d
						break
					}
				}
			}
		}
	}

	if outputDev != nil {
		fmt.Printf("VOICE: playbackManager - outputDevice selected: %v\n", outputDev.Name)
		params := portaudio.StreamParameters{
			Output: portaudio.StreamDeviceParameters{
				Device:   outputDev,
				Channels: channels,
				Latency:  outputDev.DefaultLowOutputLatency,
			},
			SampleRate:      float64(sampleRate),
			FramesPerBuffer: framesPerBuffer,
		}
		s, err = portaudio.OpenStream(params, buf)

		// If selected device fails, fallback to default
		if err != nil {
			fmt.Printf("VOICE: playbackManager - selected device '%s' failed: %v, falling back to default\n", outputDev.Name, err)
			s, err = portaudio.OpenDefaultStream(0, channels, float64(sampleRate), framesPerBuffer, buf)
		}
	} else {
		fmt.Println("VOICE: playbackManager - no output device selected, using default")
		s, err = portaudio.OpenDefaultStream(0, channels, float64(sampleRate), framesPerBuffer, buf)
	}
	if err != nil {
		fmt.Printf("VOICE: playbackManager - stream open failed (no fallback available): %v\n", err)
		vc.audioMu.Unlock()
		return
	}
	s.Start()
	vc.outputStream = s
	fmt.Println("VOICE: playbackManager - stream ready")
	vc.audioMu.Unlock()

	// Cleanup when disconnecting
	defer func() {
		vc.audioMu.Lock()
		if vc.outputStream == s {
			s.Stop()
			s.Close()
			vc.outputStream = nil
		}
		vc.audioMu.Unlock()
	}()

	// Loop: receive audio from all peers and write to output stream
	framesSent := 0
	for vc.isConnected {
		select {
		case audioData, ok := <-vc.audioQueue:
			if !ok {
				return
			}
			framesSent++
			if framesSent%100 == 0 {
				fmt.Printf("VOICE: playbackManager processed %d frames, first sample: %d\n", framesSent, audioData[0])
			}

			// Copy audio data to buffer
			if len(audioData) > len(buf) {
				fmt.Printf("VOICE: playbackManager - audio chunk larger than buffer (%d > %d)\n", len(audioData), len(buf))
				continue
			}
			copy(buf, audioData)

			// Write to output stream
			if err := s.Write(); err != nil {
				fmt.Printf("VOICE: playbackManager - write error: %v\n", err)
				// Try to reopen stream with current device settings
				vc.audioMu.Lock()
				s.Stop()
				s.Close()

				// Try to reopen with current device setting using prefix matching
				var reopenDev *portaudio.DeviceInfo
				if vc.outputDeviceName != "" {
					if devices, err := portaudio.Devices(); err == nil {
						for _, d := range devices {
							if strings.HasPrefix(vc.outputDeviceName, d.Name) || strings.HasPrefix(d.Name, vc.outputDeviceName) {
								reopenDev = d
								break
							}
						}
					}
				}

				if reopenDev != nil {
					fmt.Printf("VOICE: playbackManager - attempting to reopen with device: %v\n", reopenDev.Name)
					params := portaudio.StreamParameters{
						Output: portaudio.StreamDeviceParameters{
							Device:   reopenDev,
							Channels: channels,
							Latency:  reopenDev.DefaultLowOutputLatency,
						},
						SampleRate:      float64(sampleRate),
						FramesPerBuffer: framesPerBuffer,
					}
					s, err = portaudio.OpenStream(params, buf)
					if err != nil {
						fmt.Printf("VOICE: playbackManager - reopen failed, trying default: %v\n", err)
						s, err = portaudio.OpenDefaultStream(0, channels, float64(sampleRate), framesPerBuffer, buf)
					}
				} else {
					fmt.Println("VOICE: playbackManager - using default device for recovery")
					s, err = portaudio.OpenDefaultStream(0, channels, float64(sampleRate), framesPerBuffer, buf)
				}
				if err == nil {
					s.Start()
					vc.outputStream = s
					fmt.Println("VOICE: playbackManager - stream reopened successfully")
				} else {
					fmt.Printf("VOICE: playbackManager - failed to reopen stream: %v\n", err)
				}
				vc.audioMu.Unlock()
			}
		default:
			// No audio available, skip this iteration to avoid busy waiting
			time.Sleep(1 * time.Millisecond)
		}
	}
}

// RestartAudio signals audio to reopen streams with new devices
// Gracefully handles failures (e.g., new input device can't be opened)
func (vc *VoiceClient) RestartAudio() {
	fmt.Println("VOICE: RestartAudio called - closing old input stream")

	// Close old input stream
	vc.audioMu.Lock()
	if vc.stream != nil {
		fmt.Println("VOICE: RestartAudio - stopping and closing old stream")
		vc.stream.Stop()
		vc.stream.Close()
		vc.stream = nil
	}
	vc.audioMu.Unlock()

	// IMPORTANT: Reset inputDeviceName to empty string to force re-detection on restart
	// This prevents stale device pointers after device switches
	fmt.Println("VOICE: RestartAudio - resetting input device name to force re-detection")
	vc.mu.Lock()
	vc.inputDeviceName = ""
	vc.mu.Unlock()

	// List available devices for debugging
	if devices, err := portaudio.Devices(); err == nil {
		fmt.Printf("VOICE: RestartAudio - available input devices: %d\n", len(devices))
		for i, d := range devices {
			if d.MaxInputChannels > 0 {
				fmt.Printf("  [%d] %s (max channels: %d)\n", i, d.Name, d.MaxInputChannels)
			}
		}
	}

	// Give old captureMic goroutine time to exit before relaunching.
	time.Sleep(80 * time.Millisecond)

	vc.mu.Lock()
	micTracks := make([]*webrtc.TrackLocalStaticSample, len(vc.micTracks))
	copy(micTracks, vc.micTracks)
	vc.mu.Unlock()

	// Restart captureMic with new input device
	// If it fails to open, it will retry or fall back to default
	fmt.Printf("VOICE: RestartAudio - restarting %d capture goroutines\n", len(micTracks))
	for _, t := range micTracks {
		go vc.captureMic(t)
	}
}

// RestartPlayback restarts the output stream with the new output device
func (vc *VoiceClient) RestartPlayback() {
	fmt.Println("VOICE: RestartPlayback called - restarting output stream")

	// Signal playbackManager to close and restart
	vc.audioMu.Lock()
	if vc.outputStream != nil {
		fmt.Println("VOICE: RestartPlayback - closing old output stream")
		vc.outputStream.Stop()
		vc.outputStream.Close()
		vc.outputStream = nil
	}
	vc.audioMu.Unlock()

	// Give playbackManager time to notice the stream is closed and restart
	time.Sleep(50 * time.Millisecond)

	fmt.Println("VOICE: RestartPlayback - old stream closed, playbackManager will restart")
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

	// Flush any queued ICE candidates now that remote description is set
	vc.iceQueueMu.Lock()
	queuedCandidates := vc.iceCandidateQueues[fromNick]
	delete(vc.iceCandidateQueues, fromNick)
	vc.iceQueueMu.Unlock()

	for _, candidate := range queuedCandidates {
		if err := pc.AddICECandidate(*candidate); err != nil {
			fmt.Printf("VOICE: handleOffer - AddICECandidate (queued) error: %v\n", err)
		}
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
		return
	}

	// Flush any queued ICE candidates now that remote description is set
	vc.iceQueueMu.Lock()
	queuedCandidates := vc.iceCandidateQueues[fromNick]
	delete(vc.iceCandidateQueues, fromNick)
	vc.iceQueueMu.Unlock()

	for _, candidate := range queuedCandidates {
		if err := pc.AddICECandidate(*candidate); err != nil {
			fmt.Printf("VOICE: handleAnswer - AddICECandidate (queued) error: %v\n", err)
		}
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

	// Try to add ICE candidate
	err = pc.AddICECandidate(candidate)
	if err != nil {
		// If it fails with InvalidStateError, queue it for later
		if err.Error() == "InvalidStateError: remote description is not set" {
			vc.iceQueueMu.Lock()
			vc.iceCandidateQueues[fromNick] = append(vc.iceCandidateQueues[fromNick], &candidate)
			vc.iceQueueMu.Unlock()
			fmt.Printf("VOICE: handleICE - queued candidate from %s (remote description not set yet)\n", fromNick)
			return
		}
		fmt.Printf("VOICE: handleICE - AddICECandidate error: %v\n", err)
	}
}

// calculateRMS computes root mean square level of audio samples
// For int16-scale float32 audio, returns values typically in 0-32768 range
func calculateRMS(buf []float32) float32 {
	if len(buf) == 0 {
		return 0
	}
	var sum float32
	for _, s := range buf {
		sum += s * s
	}
	return float32(math.Sqrt(float64(sum / float32(len(buf)))))
}

// applyAGC normalizes audio gain to target RMS level
// Works with int16-scale float32 audio (values typically 0-32768 range)
// Applies exponential smoothing to prevent sudden level jumps and reduce echo
// targetRMS: desired RMS level in int16 scale
func (vc *VoiceClient) applyAGC(buf []float32, targetRMS float32) {
	const smoothingFactor = 0.1 // even slower response time

	rms := calculateRMS(buf)

	// Always apply some normalization, even on quiet frames
	// Avoid division by zero on complete silence
	if rms < 1.0 {
		return
	}

	// Calculate desired gain to reach target RMS
	desiredGain := targetRMS / rms

	// Clamp gain: allow 0.7x to 2.5x (very conservative)
	// This just smooths out volume variations, not aggressive normalization
	if desiredGain > 2.5 {
		desiredGain = 2.5 // up to +8dB
	} else if desiredGain < 0.7 {
		desiredGain = 0.7 // down to -3dB
	}

	// Apply exponential smoothing - very gradual
	vc.agcCurrentGain = (1.0-smoothingFactor)*vc.agcCurrentGain + smoothingFactor*desiredGain

	// Apply gain with clipping prevention
	for i := range buf {
		buf[i] *= vc.agcCurrentGain
		if buf[i] > 32767 {
			buf[i] = 32767
		} else if buf[i] < -32768 {
			buf[i] = -32768
		}
	}
}

// SetPeerVolume sets the volume multiplier for a specific peer (1.0 = 100%, range 0.0-2.0)
func (vc *VoiceClient) SetPeerVolume(nick string, volume float32) {
	// Clamp volume to 0.0-2.0 range
	if volume < 0.0 {
		volume = 0.0
	} else if volume > 2.0 {
		volume = 2.0
	}

	vc.volumeMu.Lock()
	defer vc.volumeMu.Unlock()

	if volume == 1.0 {
		// Remove entry if setting to default
		delete(vc.peerVolumes, nick)
		fmt.Printf("VOICE: SetPeerVolume - reset volume for %s to default (100%%)\n", nick)
	} else {
		vc.peerVolumes[nick] = volume
		percentage := int(volume * 100)
		fmt.Printf("VOICE: SetPeerVolume - set volume for %s to %d%%\n", nick, percentage)
	}
}

// GetPeerVolume gets the volume multiplier for a specific peer (defaults to 1.0 = 100%)
func (vc *VoiceClient) GetPeerVolume(nick string) float32 {
	vc.volumeMu.Lock()
	defer vc.volumeMu.Unlock()

	if volume, exists := vc.peerVolumes[nick]; exists {
		return volume
	}
	return 1.0 // default to 100%
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

	// Look up device by name using prefix matching (handles truncated names)
	var inputDev *portaudio.DeviceInfo
	fmt.Printf("VOICE: captureMic - looking for input device by prefix: '%s'\n", vc.inputDeviceName)
	if vc.inputDeviceName != "" {
		if devices, _ := portaudio.Devices(); devices != nil {
			fmt.Printf("VOICE: captureMic - searching devices for match\n")
			for _, d := range devices {
				if d.MaxInputChannels > 0 {
					// Use prefix matching to handle truncated device names
					if strings.HasPrefix(vc.inputDeviceName, d.Name) || strings.HasPrefix(d.Name, vc.inputDeviceName) {
						fmt.Printf("VOICE: captureMic - MATCH FOUND: '%s'\n", d.Name)
						inputDev = d
						break
					}
				}
			}
		}
	}

	// Attempt with selected device if found
	if inputDev != nil {
		// use selected device
		fmt.Printf("VOICE: captureMic - using device: %v (channels: %d, max input channels: %d)\n",
			inputDev.Name, channels, inputDev.MaxInputChannels)
		params := portaudio.StreamParameters{
			Input: portaudio.StreamDeviceParameters{
				Device:   inputDev,
				Channels: channels,
				Latency:  inputDev.DefaultLowInputLatency,
			},
			SampleRate:      float64(sampleRate),
			FramesPerBuffer: framesPerBuffer,
		}
		stream, err = portaudio.OpenStream(params, buf)

		// If selected device fails, fallback to default
		if err != nil {
			fmt.Printf("VOICE: captureMic - selected device failed: %v, falling back to default\n", err)
			stream, err = portaudio.OpenDefaultStream(channels, 0, float64(sampleRate), framesPerBuffer, buf)
		}
	} else {
		fmt.Println("VOICE: captureMic - using default input device")
		// Get and log default device info
		if defDev, err := portaudio.DefaultInputDevice(); err == nil {
			fmt.Printf("VOICE: captureMic - default device: %v (channels: %d)\n",
				defDev.Name, defDev.MaxInputChannels)
		}
		stream, err = portaudio.OpenDefaultStream(channels, 0, float64(sampleRate), framesPerBuffer, buf)
	}
	if err != nil {
		fmt.Printf("VOICE: captureMic - stream open failed (no fallback available): %v\n", err)
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

	frameCount := 0
	zeroFrameCount := 0
	for vc.isConnected {
		readErr := stream.Read()
		if readErr != nil {
			fmt.Println("voice: read error (device disconnected?):", readErr)
			return
		}

		frameCount++

		// Check for stuck/silent device - if we get too many all-zero frames, something is wrong
		allZero := true
		for _, s := range buf {
			if s != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			zeroFrameCount++
		} else {
			zeroFrameCount = 0 // reset counter
		}

		if zeroFrameCount > 500 && zeroFrameCount%100 == 0 {
			fmt.Printf("VOICE: WARNING - Device returning all zeros for %d+ consecutive frames! Device may be disconnected.\n", zeroFrameCount)
		}

		if frameCount%100 == 0 {
			fmt.Printf("voice: frame %d read successfully, first sample: %d (all-zero: %v)\n", frameCount, buf[0], allZero)
		}

		// convert int16 → float32
		for i, s := range buf {
			floatBuf[i] = float32(s)
		}

		// apply noise suppression
		vad := denoiser.Process(floatBuf)

		// apply automatic gain control (AGC) to normalize audio level smoothly
		vc.applyAGC(floatBuf, vc.agcTargetLevel)

		// convert back float32 → int16
		for i, f := range floatBuf {
			buf[i] = int16(f)
		}

		// VAD: threshold (0.7) to filter out echo and background noise without cutting speech
		isVoice := vad > 0.7

		if frameCount%100 == 0 {
			fmt.Printf("voice: VAD value: %.4f, isVoice: %v\n", vad, isVoice)
		}

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

		// VAD gate — drop silent frames and reset accumulator to avoid partial chunks.
		if !isVoice {
			accumCount = 0
			continue
		}

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

	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		fmt.Println("voice: opus decoder error:", err)
		return
	}

	framesReceived := 0
	// Read RTP packets, decode, and send to playbackManager queue
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

		framesReceived++
		if framesReceived%100 == 0 {
			fmt.Printf("VOICE: playAudio received %d RTP frames, first decoded sample: %d\n", framesReceived, buf[0])
		}

		// Copy the decoded audio before sending to avoid buffer reuse issues
		audioCopy := make([]int16, len(buf))
		copy(audioCopy, buf)

		// Send decoded audio to playback manager
		// Non-blocking send to avoid deadlock if queue is full
		select {
		case vc.audioQueue <- audioCopy:
			// Sent successfully
		default:
			// Queue full, drop frame to maintain responsiveness
			fmt.Println("VOICE: audio queue full, dropping frame")
		}
	}
}
