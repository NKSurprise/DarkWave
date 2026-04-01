package main

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/gordonklaus/portaudio"
)

type FriendStatus struct {
	nick   string
	online bool
}

type RoomWithVoice struct {
	name          string
	voiceChannels []string
}

type VoiceMember struct {
	nick     string
	speaking bool
}

func chatScreen(w fyne.Window, conn *Connection, myNick string) fyne.CanvasObject {
	var msgs []string
	var friends []FriendStatus
	var currentRoom string
	var currentDMKey []byte
	var isDM bool
	var members []string
	var roomsWithVoice []RoomWithVoice
	var voiceClient *VoiceClient
	var voiceMembers []VoiceMember
	var savedInputDevice *portaudio.DeviceInfo
	var savedOutputDevice *portaudio.DeviceInfo

	// -- FRIENDS HOME LIST --
	friendsHomeList := widget.NewList(
		func() int { return len(friends) },
		func() fyne.CanvasObject {
			dot := widget.NewLabel("●")
			name := widget.NewLabel("")
			return container.NewHBox(dot, name)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*fyne.Container)
			dot := row.Objects[0].(*widget.Label)
			name := row.Objects[1].(*widget.Label)
			name.SetText(friends[i].nick)
			if friends[i].online {
				dot.SetText("●")
			} else {
				dot.SetText("○")
			}
		},
	)

	friendsHome := container.NewBorder(
		widget.NewLabelWithStyle("Friends", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		friendsHomeList,
	)

	// -- MESSAGES --
	msgList := widget.NewList(
		func() int { return len(msgs) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(msgs[i])
			o.(*widget.Label).Wrapping = fyne.TextWrapWord
		},
	)

	// -- INPUT --
	input := widget.NewEntry()
	input.SetPlaceHolder("Message...")

	sendMsg := func() {
		text := input.Text
		if text == "" {
			return
		}
		if isDM && currentDMKey != nil {
			encrypted, err := encryptMsg([]byte(text), currentDMKey)
			if err == nil {
				conn.send(encrypted)
			}
		} else {
			conn.send(text)
		}
		input.SetText("")
	}

	input.OnSubmitted = func(_ string) { sendMsg() }
	sendBtn := widget.NewButton("Send", func() { sendMsg() })
	sendBtn.Importance = widget.HighImportance

	inputRow := container.NewBorder(nil, nil, nil, sendBtn, input)
	chatArea := container.NewBorder(nil, inputRow, nil, nil, msgList)

	// -- ON FRIEND CLICK --

	// -- MEMBERS PANEL --
	membersList := widget.NewList(
		func() int { return len(members) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText("● " + members[i])
		},
	)

	membersPanel := container.NewBorder(
		widget.NewLabelWithStyle("Members", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		membersList,
	)

	// chat + members side by side
	chatWithMembers := container.NewHSplit(chatArea, membersPanel)

	// center panel — friends home by default
	center := container.NewStack(friendsHome)

	friendsHomeList.OnSelected = func(i widget.ListItemID) {
		targetNick := friends[i].nick
		conn.send("/dm " + targetNick)
		currentDMKey = deriveKey(myNick, targetNick)
		isDM = true
		msgs = []string{}
		members = []string{}
		membersList.Refresh()
		msgList.Refresh()
		center.Objects = []fyne.CanvasObject{chatArea}
		center.Refresh()
		friendsHomeList.Unselect(i)
	}

	voiceMembersList := widget.NewList(
		func() int { return len(voiceMembers) },
		func() fyne.CanvasObject {
			indicator := widget.NewLabel("○")
			name := widget.NewLabel("")
			return container.NewHBox(indicator, name)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*fyne.Container)
			indicator := row.Objects[0].(*widget.Label)
			name := row.Objects[1].(*widget.Label)
			name.SetText(voiceMembers[i].nick)
			if voiceMembers[i].speaking {
				indicator.SetText("●")
			} else {
				indicator.SetText("○")
			}
		},
	)

	var leaveVoiceBtn *widget.Button
	leaveVoiceBtn = widget.NewButton("🔇 Leave Voice", func() {
		if voiceClient != nil {
			voiceClient.LeaveChannel()
			voiceClient = nil
		}
		voiceMembers = []VoiceMember{}
		voiceMembersList.Refresh()
		leaveVoiceBtn.Hide()
	})
	leaveVoiceBtn.Hide()

	leaveBtn := widget.NewButton("🚪 Leave Room", func() {
		conn.send("/leave")
		msgs = []string{}
		members = []string{}
		membersList.Refresh()
		msgList.Refresh()
		// refresh rooms list
		center.Objects = []fyne.CanvasObject{friendsHome}
		center.Refresh()
	})
	leaveBtn.Importance = widget.DangerImportance
	leaveBtn.Hide()

	roomsList := widget.NewList(
		func() int {
			total := 0
			for _, r := range roomsWithVoice {
				total++                       // room name
				total += len(r.voiceChannels) // voice channels
			}
			return total
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			label := o.(*widget.Label)
			// find which item index i corresponds to
			idx := 0
			for _, r := range roomsWithVoice {
				if idx == i {
					label.SetText("# " + r.name)
					label.TextStyle = fyne.TextStyle{Bold: true}
					return
				}
				idx++
				for _, vc := range r.voiceChannels {
					if idx == i {
						label.SetText("  🔊 " + vc)
						label.TextStyle = fyne.TextStyle{}
						return
					}
					idx++
				}
			}
		},
	)

	voicePanel := container.NewBorder(
		widget.NewLabelWithStyle("Voice", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		voiceMembersList,
	)

	rightPanel := container.NewVSplit(membersPanel, voicePanel)
	rightPanel.SetOffset(0.6)
	chatWithMembers = container.NewHSplit(chatArea, rightPanel)

	roomsList.OnSelected = func(i widget.ListItemID) {
		// figure out what was clicked
		idx := 0
		for _, r := range roomsWithVoice {
			if idx == i {
				currentRoom = r.name
				isDM = false
				currentDMKey = nil
				members = []string{}
				membersList.Refresh()
				conn.send("/join " + r.name)
				conn.send("/voicechannels")
				msgs = []string{}
				msgList.Refresh()
				center.Objects = []fyne.CanvasObject{chatWithMembers}
				center.Refresh()
				chatWithMembers.SetOffset(0.75)
				leaveBtn.Show()
				currentRoom = r.name
				conn.send("/voicechannels")
				return
			}
			idx++
			for _, vc := range r.voiceChannels {
				if idx == i {
					go func(channelName string) {
						//var inputDev, outputDev *portaudio.DeviceInfo
						if voiceClient != nil {
							savedInputDevice = voiceClient.inputDevice
							savedOutputDevice = voiceClient.outputDevice
							voiceClient.LeaveChannel()
							voiceClient = nil
						}
						voiceClient = NewVoiceClient(myNick)
						voiceClient.inputDevice = savedInputDevice
						voiceClient.outputDevice = savedOutputDevice

						// set ALL callbacks before joining
						voiceClient.onSpeaking = func(nick string, speaking bool) {
							fyne.Do(func() {
								for i, m := range voiceMembers {
									if m.nick == nick {
										voiceMembers[i].speaking = speaking
										voiceMembersList.Refresh()
										return
									}
								}
							})
						}
						voiceClient.onMemberJoin = func(nick string) {
							fyne.Do(func() {
								voiceMembers = append(voiceMembers, VoiceMember{nick: nick})
								voiceMembersList.Refresh()
							})
						}
						voiceClient.onMemberLeave = func(nick string) {
							fyne.Do(func() {
								for i, m := range voiceMembers {
									if m.nick == nick {
										voiceMembers = append(voiceMembers[:i], voiceMembers[i+1:]...)
										break
									}
								}
								voiceMembersList.Refresh()
							})
						}

						wsAddr := "ws://localhost:3001"
						roomName := strings.TrimPrefix(r.name, "#")
						err := voiceClient.JoinChannel(wsAddr, roomName+"/"+channelName)
						if err != nil {
							fmt.Println("voice join error:", err)
							return
						}
						fyne.Do(func() {
							leaveVoiceBtn.Show()
							voiceMembers = []VoiceMember{{nick: myNick}}
							voiceMembersList.Refresh()
						})
					}(vc)
					return
				}
				idx++
			}
		}
	}
	friendsBtn := widget.NewButton("👥 Friends", func() {
		isDM = false
		currentDMKey = nil
		members = []string{}
		membersList.Refresh()
		roomsList.UnselectAll()
		leaveBtn.Hide()
		center.Objects = []fyne.CanvasObject{friendsHome}
		center.Refresh()
	})

	settingsBtn := widget.NewButton("⚙ Audio", func() {
		showAudioSettings(w, voiceClient, func(input, output *portaudio.DeviceInfo) {
			savedInputDevice = input
			savedOutputDevice = output
			if voiceClient != nil {
				voiceClient.inputDevice = input
				voiceClient.outputDevice = output
			}
		})
	})

	bottomBtns := container.NewVBox(leaveVoiceBtn, leaveBtn, settingsBtn, friendsBtn)

	roomsPanel := container.NewBorder(
		widget.NewLabelWithStyle("Rooms", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		bottomBtns,
		nil, nil,
		roomsList,
	)

	// -- INCOMING MESSAGES --
	go func() {
		for msg := range conn.incoming {
			msg := msg // capture
			fyne.Do(func() {
				if strings.HasPrefix(msg, "** friends: ") {
					raw := strings.TrimPrefix(msg, "** friends: ")
					if raw == "(none)" {
						friends = []FriendStatus{}
					} else {
						for _, nick := range strings.Split(raw, ", ") {
							friends = append(friends, FriendStatus{nick: nick, online: false})
						}
					}
					friendsHomeList.Refresh()
					conn.send("/online")
					return
				}
				if strings.HasPrefix(msg, "** rooms: ") {
					raw := strings.TrimPrefix(msg, "** rooms: ")
					if raw != "(none)" {
						allRooms := strings.Split(raw, ", ")
						roomsWithVoice = []RoomWithVoice{}
						for _, r := range allRooms {
							if !strings.HasPrefix(r, "dm:") {
								roomsWithVoice = append(roomsWithVoice, RoomWithVoice{name: r})
							}
						}
						roomsList.Refresh()
					}
					return
				}
				if strings.HasPrefix(msg, "** voice channels: ") {
					raw := strings.TrimPrefix(msg, "** voice channels: ")
					if raw != "(none)" {
						for i, r := range roomsWithVoice {
							if r.name == currentRoom {
								roomsWithVoice[i].voiceChannels = strings.Split(raw, ", ")
								break
							}
						}
					}
					roomsList.Refresh()
					return
				}
				if strings.HasPrefix(msg, "** voicechannel added: ") {
					name := strings.TrimPrefix(msg, "** voicechannel added: ")
					for i, r := range roomsWithVoice {
						if r.name == currentRoom {
							roomsWithVoice[i].voiceChannels = append(roomsWithVoice[i].voiceChannels, name)
							break
						}
					}
					roomsList.Refresh()
					return
				}
				if strings.HasPrefix(msg, "** status: ") {
					parts := strings.Fields(strings.TrimPrefix(msg, "** status: "))
					if len(parts) == 2 {
						nick, status := parts[0], parts[1]
						for i, f := range friends {
							if f.nick == nick {
								friends[i].online = status == "online"
								break
							}
						}
						friendsHomeList.Refresh()
					}
					return
				}
				if strings.HasPrefix(msg, "** members: ") {
					raw := strings.TrimPrefix(msg, "** members: ")
					members = strings.Split(raw, ", ")
					membersList.Refresh()
					return
				}
				if strings.HasPrefix(msg, "** joined: ") {
					nick := strings.TrimPrefix(msg, "** joined: ")
					members = append(members, nick)
					membersList.Refresh()
					return
				}
				if strings.HasPrefix(msg, "** joined ") {
					return
				}
				if strings.HasPrefix(msg, "** left: ") {
					nick := strings.TrimPrefix(msg, "** left: ")
					for i, m := range members {
						if m == nick {
							members = append(members[:i], members[i+1:]...)
							break
						}
					}
					membersList.Refresh()
					return
				}
				if strings.HasPrefix(msg, "** left ") {
					return
				}
				if strings.HasPrefix(msg, "** you left") {
					conn.send("/rooms")
					leaveBtn.Hide()
					msgs = []string{}
					msgList.Refresh()
					center.Objects = []fyne.CanvasObject{friendsHome}
					center.Refresh()
					return
				}
				if strings.HasPrefix(msg, "** dm: ") {
					parts := strings.Fields(strings.TrimPrefix(msg, "** dm: "))
					if len(parts) == 2 {
						isDM = true
						currentDMKey = deriveKey(myNick, parts[0])
					}
					return
				}
				if strings.HasPrefix(msg, "** connected.") ||
					strings.HasPrefix(msg, "** welcome,") {
					return
				}
				if isDM && currentDMKey != nil {
					if idx := strings.Index(msg, ") "); idx != -1 {
						prefix := msg[:idx+2]
						ciphertext := msg[idx+2:]
						decrypted, err := decryptMsg(ciphertext, currentDMKey)
						if err == nil {
							msg = prefix + decrypted
						}
					}
				}
				msgs = append(msgs, msg)
				msgList.Refresh()
				msgList.ScrollToBottom()
			})
		}
	}()

	conn.send("/friends")
	conn.send("/rooms")

	fullLayout := container.NewHSplit(roomsPanel, center)
	go func() {
		fyne.Do(func() {
			fullLayout.SetOffset(0.2)
		})
	}()
	return fullLayout
}
