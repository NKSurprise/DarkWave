package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type FriendStatus struct {
	nick   string
	online bool
}

func chatScreen(w fyne.Window, conn *Connection, myNick string) fyne.CanvasObject {
	var msgs []string
	var friends []FriendStatus
	var rooms []string
	var currentDMKey []byte
	var isDM bool
	var members []string

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

	// -- ROOMS SIDEBAR --
	roomsList := widget.NewList(
		func() int { return len(rooms) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText("# " + rooms[i])
		},
	)

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

	bottomBtns := container.NewVBox(leaveBtn, friendsBtn)

	roomsPanel := container.NewBorder(
		widget.NewLabelWithStyle("Rooms", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		bottomBtns,
		nil, nil,
		roomsList,
	)

	roomsList.OnSelected = func(i widget.ListItemID) {
		isDM = false
		currentDMKey = nil
		members = []string{}
		membersList.Refresh()
		conn.send("/join " + rooms[i])
		msgs = []string{}
		msgList.Refresh()
		center.Objects = []fyne.CanvasObject{chatWithMembers} // ← chatWithMembers not chatArea
		center.Refresh()
		chatWithMembers.SetOffset(0.75)
		leaveBtn.Show()
	}

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
						rooms = []string{}
						for _, r := range allRooms {
							if !strings.HasPrefix(r, "dm:") {
								rooms = append(rooms, r)
							}
						}
						roomsList.Refresh()
					}
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
