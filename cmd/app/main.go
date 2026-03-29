package main

import (
	_ "embed"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

//go:embed Images/gemini-2.png
var iconBytes []byte

var currentApp fyne.App

func main() {
	currentApp = app.New()
	currentApp.SetIcon(fyne.NewStaticResource("Icon.png", iconBytes))
	currentApp.Settings().SetTheme(NewDarkWaveTheme(AccentBlue))

	w := currentApp.NewWindow("DarkWave")
	w.Resize(fyne.NewSize(400, 520))
	w.CenterOnScreen()
	w.SetContent(loginScreen(w))
	w.ShowAndRun()
}

func loginScreen(w fyne.Window) fyne.CanvasObject {
	title := widget.NewLabel("🌊 DarkWave")
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	subtitle := widget.NewLabel("connect to the wave")
	subtitle.Alignment = fyne.TextAlignCenter

	nickEntry := widget.NewEntry()
	nickEntry.SetPlaceHolder("nickname")

	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder("password")

	statusLabel := widget.NewLabel("")
	statusLabel.Alignment = fyne.TextAlignCenter

	// theme picker
	var selectedAccent color.Color = AccentBlue
	themeLabel := widget.NewLabel("accent color:")

	redBtn := widget.NewButton("Red", func() {
		selectedAccent = AccentRed
		currentApp.Settings().SetTheme(NewDarkWaveTheme(AccentRed))
	})
	blueBtn := widget.NewButton("Blue", func() {
		selectedAccent = AccentBlue
		currentApp.Settings().SetTheme(NewDarkWaveTheme(AccentBlue))
		_ = selectedAccent
	})
	greenBtn := widget.NewButton("Green", func() {
		selectedAccent = AccentGreen
		currentApp.Settings().SetTheme(NewDarkWaveTheme(AccentGreen))
		_ = selectedAccent
	})
	purpleBtn := widget.NewButton("Purple", func() {
		selectedAccent = AccentPurple
		currentApp.Settings().SetTheme(NewDarkWaveTheme(AccentPurple))
		_ = selectedAccent
	})

	colorRow := container.NewHBox(redBtn, blueBtn, greenBtn, purpleBtn)

	connectBtn := widget.NewButton("Connect", func() {
		nick := nickEntry.Text
		pass := passEntry.Text

		if nick == "" || pass == "" {
			statusLabel.SetText("⚠ please enter nick and password")
			return
		}

		statusLabel.SetText("connecting...")

		go func() {
			conn, err := connect("localhost:3000", nick, pass)
			if err != nil {
				fyne.Do(func() {
					statusLabel.SetText("⚠ could not connect to server")
				})
				return
			}
			fyne.Do(func() {
				w.Resize(fyne.NewSize(1000, 650))
				w.CenterOnScreen()
				w.SetContent(chatScreen(w, conn, nick))
			})
		}()
	})
	connectBtn.Importance = widget.HighImportance

	form := container.NewVBox(
		widget.NewSeparator(),
		nickEntry,
		passEntry,
		widget.NewSeparator(),
		themeLabel,
		colorRow,
		widget.NewSeparator(),
		connectBtn,
		statusLabel,
	)

	content := container.NewVBox(
		title,
		subtitle,
		form,
	)

	return container.NewCenter(content)
}
